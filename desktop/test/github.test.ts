import { test } from "node:test";
import assert from "node:assert/strict";

import {
  DISCONNECT_WORD,
  GITHUB_POLL_MS,
  canDisconnect,
  formatRemaining,
  githubFailure,
  isGitHubUrl,
  parseGitHubConnection,
  parseGitHubPrompt,
  pendingConnection,
  remainingSeconds,
  shouldPoll,
} from "../src/renderer/lib/github.ts";

const INSTALL = "https://github.com/apps/kadran-deploy/installations/new";
const prompt = (over: Record<string, unknown> = {}) => ({
  user_code: "WDJB-MJHT",
  verification_uri: "https://github.com/login/device",
  install_url: INSTALL,
  expires_at: "2026-11-12T10:15:00Z",
  ...over,
});
const connected = (over: Record<string, unknown> = {}) => ({
  state: "STATE_CONNECTED",
  login: "octo",
  pending: null,
  installations: [{ account: "octo", all_repositories: false, truncated: false, repositories: [{ full_name: "octo/site", private: true }, { full_name: "octo/docs", private: false }] }],
  connected_at: "2026-11-12T09:00:00Z",
  failure: "GIT_HUB_FAILURE_UNSPECIFIED",
  reconnect_required: false,
  install_url: INSTALL,
  ...over,
});

test("parseGitHubConnection reads the four states", () => {
  assert.equal(parseGitHubConnection({ connection: { state: "STATE_NOT_CONNECTED" } }).state, "not_connected");
  const pending = parseGitHubConnection({ connection: { state: "STATE_PENDING", pending: prompt() } });
  assert.equal(pending.state, "pending");
  assert.equal(pending.prompt?.userCode, "WDJB-MJHT");
  assert.equal(pending.prompt?.expiresAt, Date.parse("2026-11-12T10:15:00Z"));
  const ok = parseGitHubConnection({ connection: connected() });
  assert.equal(ok.state, "connected");
  assert.equal(ok.login, "octo");
  assert.equal(ok.installations[0]?.repositories.length, 2);
  const failed = parseGitHubConnection({ connection: { state: "STATE_FAILED", failure: "GIT_HUB_FAILURE_ACCESS_DENIED" } });
  assert.equal(failed.state, "failed");
  assert.equal(failed.failureKey, "github.failure_access_denied");
});

test("an unknown, unspecified or malformed state counts as failed", () => {
  for (const raw of [{ connection: { state: "STATE_SOMETHING_NEW" } }, { connection: { state: "STATE_UNSPECIFIED" } }, { connection: {} }, { connection: null }, {}, null, "x", []]) {
    const parsed = parseGitHubConnection(raw);
    assert.equal(parsed.state, "failed", JSON.stringify(raw));
    assert.equal(parsed.failureKey, "github.failure_internal");
  }
});

test("every failure enum maps to its own message key", () => {
  const keys = new Map([
    ["GIT_HUB_FAILURE_ACCESS_DENIED", "github.failure_access_denied"],
    ["GIT_HUB_FAILURE_CODE_EXPIRED", "github.failure_code_expired"],
    ["GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED", "github.failure_device_flow_disabled"],
    ["GIT_HUB_FAILURE_BAD_CLIENT", "github.failure_bad_client"],
    ["GIT_HUB_FAILURE_NETWORK", "github.failure_network"],
    ["GIT_HUB_FAILURE_RATE_LIMITED", "github.failure_rate_limited"],
    ["GIT_HUB_FAILURE_INTERNAL", "github.failure_internal"],
    ["GIT_HUB_FAILURE_UNSPECIFIED", "github.failure_internal"],
    ["unexpected", "github.failure_internal"],
  ]);
  for (const [enumName, key] of keys) {
    assert.equal(parseGitHubConnection({ connection: { state: "STATE_FAILED", failure: enumName } }).failureKey, key, enumName);
  }
});

test("a user code that is not XXXX-XXXX is never shown", () => {
  for (const code of ["WDJB-MJHT", "1234-ABCD", "A1B2-C3D4"]) {
    assert.equal(parseGitHubPrompt({ prompt: prompt({ user_code: code }) })?.userCode, code);
  }
  for (const code of ["wdjb-mjht", "WDJBMJHT", "WDJB-MJHT1", "WDJ-MJHT", "WDJB MJHT", "WDJB-MJH\n", " WDJB-MJHT", "WDJB-MJHT ", "<b>A</b>", "", "ÇDJB-MJHT", 12345678, null]) {
    assert.equal(parseGitHubPrompt({ prompt: prompt({ user_code: code }) })?.userCode, null, JSON.stringify(code));
    const viaStatus = parseGitHubConnection({ connection: { state: "STATE_PENDING", pending: prompt({ user_code: code }) } });
    assert.equal(viaStatus.prompt?.userCode, null);
  }
});

test("only allowlisted GitHub addresses survive parsing", () => {
  const good = parseGitHubPrompt({ prompt: prompt() });
  assert.equal(good?.verificationUri, "https://github.com/login/device");
  assert.equal(good?.installUrl, INSTALL);
  const evil = parseGitHubPrompt({ prompt: prompt({ verification_uri: "https://evil.example/login/device", install_url: "https://github.com/apps/x/installations/new?next=//evil" }) });
  assert.equal(evil?.verificationUri, null);
  assert.equal(evil?.installUrl, null);
  const bad = ["", "http://github.com/login/device", "https://github.com/login/device?a=1", "https://github.com/login/device/", "https://github.com.evil.example/login/device", "https://github.com@evil.example/login/device", "https://GitHub.com/login/device", "javascript:alert(1)", "https://github.com/apps/Kadran/installations/new", "https://github.com/settings/tokens", "https://github.com/login/device\n"];
  for (const url of bad) assert.equal(isGitHubUrl(url), false, url);
  for (const url of ["https://github.com/login/device", INSTALL, "https://github.com/settings/apps/authorizations", "https://github.com/settings/installations"]) assert.equal(isGitHubUrl(url), true, url);
  assert.equal(isGitHubUrl("https://github.com/apps/" + "a".repeat(101) + "/installations/new"), false);
  assert.equal(isGitHubUrl(42), false);
});

test("the ALL repositories grant is flagged, selected ones are counted, the private badge is kept", () => {
  const all = parseGitHubConnection({ connection: connected({ installations: [{ account: "octo", all_repositories: true, truncated: false, repositories: [] }] }) });
  assert.equal(all.hasAllRepositories, true);
  assert.equal(all.installations[0]?.allRepositories, true);
  const selected = parseGitHubConnection({ connection: connected({ installations: [{ account: "octo", all_repositories: false, truncated: true, repositories: [{ full_name: "octo/a", private: true }, { full_name: "octo/b" }] }] }) });
  assert.equal(selected.hasAllRepositories, false);
  assert.equal(selected.installations[0]?.truncated, true);
  assert.deepEqual(selected.installations[0]?.repositories, [{ fullName: "octo/a", isPrivate: true }, { fullName: "octo/b", isPrivate: false }]);
  const none = parseGitHubConnection({ connection: connected({ installations: [] }) });
  assert.equal(none.installations.length, 0);
  assert.equal(none.hasAllRepositories, false);
});

test("reconnect_required and the install link are carried", () => {
  const c = parseGitHubConnection({ connection: connected({ reconnect_required: true }) });
  assert.equal(c.reconnectRequired, true);
  assert.equal(c.installUrl, INSTALL);
  assert.equal(parseGitHubConnection({ connection: connected({ install_url: "https://evil.example" }) }).installUrl, null);
  assert.equal(parseGitHubConnection({ connection: connected({ reconnect_required: "yes" }) }).reconnectRequired, false);
});

test("malformed installations and repositories are dropped, not trusted", () => {
  const c = parseGitHubConnection({ connection: connected({ installations: [null, "x", { account: 5 }, { account: "ok", repositories: [null, { full_name: 7 }, { full_name: "ok/r", private: true }] }] }) });
  assert.deepEqual(c.installations.map((i) => i.account), ["ok"]);
  assert.deepEqual(c.installations[0]?.repositories, [{ fullName: "ok/r", isPrivate: true }]);
});

test("an old server (gRPC Unimplemented) is unsupported, other errors are failures", () => {
  assert.equal(githubFailure(new Error("call failed: rpc error: code = Unimplemented desc = unknown method GetGitHubStatus")), "unsupported");
  assert.equal(githubFailure("call failed: rpc error: code = Unimplemented desc = x"), "unsupported");
  assert.equal(githubFailure(new Error("call failed: rpc error: code = Unavailable desc = connection reset")), "failed");
  assert.equal(githubFailure(new Error("ssh: connect to host down port 22: Connection refused")), "failed");
  assert.equal(githubFailure(undefined), "failed");
});

test("polling happens only while pending and the window is visible", () => {
  assert.equal(shouldPoll("pending", false), true);
  assert.equal(shouldPoll("pending", true), false);
  for (const state of ["not_connected", "connected", "failed"] as const) {
    assert.equal(shouldPoll(state, false), false, state);
    assert.equal(shouldPoll(state, true), false, state);
  }
  assert.equal(GITHUB_POLL_MS, 3000);
});

test("disconnect needs the literal word", () => {
  assert.equal(DISCONNECT_WORD, "disconnect");
  assert.equal(canDisconnect("disconnect"), true);
  for (const typed of ["", "Disconnect", "disconnect ", " disconnect", "disconne", "yes"]) assert.equal(canDisconnect(typed), false, JSON.stringify(typed));
});

test("remaining time counts down from expires_at and never goes negative", () => {
  const at = Date.parse("2026-11-12T10:15:00Z");
  assert.equal(remainingSeconds(at, at - 899_000), 899);
  assert.equal(remainingSeconds(at, at - 500), 1);
  assert.equal(remainingSeconds(at, at), 0);
  assert.equal(remainingSeconds(at, at + 60_000), 0);
  assert.equal(remainingSeconds(null, at), null);
  assert.equal(formatRemaining(899), "14:59");
  assert.equal(formatRemaining(65), "1:05");
  assert.equal(formatRemaining(0), "0:00");
});

test("connecting shows a pending state with the prompt before the first poll", () => {
  const p = parseGitHubPrompt({ prompt: prompt() });
  assert.ok(p);
  const c = pendingConnection(p);
  assert.equal(c.state, "pending");
  assert.equal(c.prompt?.userCode, "WDJB-MJHT");
  assert.equal(c.installUrl, INSTALL);
  assert.equal(shouldPoll(c.state, false), true);
});

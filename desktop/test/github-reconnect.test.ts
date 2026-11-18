import { test } from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

import { GITHUB_POLL_MS, inlineError, parseGitHubConnection, pendingExpired, rejectedNotice } from "../src/renderer/lib/github.ts";
import { startGitHubPolling, type PollEnv } from "../src/renderer/lib/github-poll.ts";

const PROMPT = { user_code: "WDJB-MJHT", verification_uri: "https://github.com/login/device", expires_at: "2026-11-12T10:15:00Z" };

// Real post-drop shapes: kadran-git deletes the identity after GitHub rejects
// the token, so the state is NOT_CONNECTED (or PENDING/FAILED once a new flow
// started) with reconnect_required. It is never CONNECTED with reconnect.
test("the reconnect notice shows in NOT_CONNECTED, PENDING and FAILED", () => {
  const shapes = [
    { state: "STATE_NOT_CONNECTED", reconnect_required: true },
    { state: "STATE_PENDING", pending: PROMPT, reconnect_required: true },
    { state: "STATE_FAILED", failure: "GIT_HUB_FAILURE_ACCESS_DENIED", reconnect_required: true },
  ];
  for (const shape of shapes) {
    const c = parseGitHubConnection({ connection: shape });
    assert.equal(c.reconnectRequired, true, shape.state);
    assert.equal(rejectedNotice(c), true, shape.state);
  }
});

test("no notice without reconnect_required, and none inline on CONNECTED", () => {
  for (const state of ["STATE_NOT_CONNECTED", "STATE_FAILED"]) {
    assert.equal(rejectedNotice(parseGitHubConnection({ connection: { state, reconnect_required: false } })), false, state);
  }
  const connected = parseGitHubConnection({ connection: { state: "STATE_CONNECTED", login: "octo", reconnect_required: true } });
  assert.equal(rejectedNotice(connected), false);
});

test("the notice text is in both languages and says GitHub rejected the connection", () => {
  const en = JSON.parse(readFileSync(new URL("../src/renderer/i18n/en.json", import.meta.url), "utf8")) as Record<string, string>;
  const tr = JSON.parse(readFileSync(new URL("../src/renderer/i18n/tr.json", import.meta.url), "utf8")) as Record<string, string>;
  assert.match(en["github.reconnect_rejected"] ?? "", /GitHub rejected the saved connection/);
  assert.ok((tr["github.reconnect_rejected"] ?? "").length > 0);
});

test("the countdown reaching 0 counts as expired; unknown or running time does not", () => {
  assert.equal(pendingExpired(0), true);
  assert.equal(pendingExpired(1), false);
  assert.equal(pendingExpired(null), false);
});

test("an error shows once: not behind the disconnect dialog", () => {
  assert.equal(inlineError("boom", true, false), "boom");
  assert.equal(inlineError("boom", true, true), null);
  assert.equal(inlineError("boom", false, false), null);
  assert.equal(inlineError(null, true, false), null);
});

interface FakeState {
  hidden: boolean;
  timers: Map<number, () => void>;
  listeners: Set<() => void>;
  cleared: number;
}

interface Fake {
  env: PollEnv;
  state: FakeState;
  fire(): void;
  visible(): void;
}

function fakeEnv(): Fake {
  const state: FakeState = { hidden: false, timers: new Map(), listeners: new Set(), cleared: 0 };
  let next = 1;
  const env: PollEnv = {
    hidden: () => state.hidden,
    setInterval: (fn, ms) => {
      assert.equal(ms, GITHUB_POLL_MS);
      state.timers.set(next, fn);
      return next++;
    },
    clearInterval: (id) => {
      state.cleared++;
      state.timers.delete(id as number);
    },
    onVisibility: (fn) => state.listeners.add(fn),
    offVisibility: (fn) => state.listeners.delete(fn),
  };
  return { env, state, fire: () => state.timers.forEach((fn) => fn()), visible: () => state.listeners.forEach((fn) => fn()) };
}

const flush = () => new Promise((r) => setImmediate(r));

test("polling reads on every interval tick while pending and visible", async () => {
  const f = fakeEnv();
  let reads = 0;
  const stop = startGitHubPolling("pending", async () => { reads++; }, f.env);
  f.fire();
  await flush();
  f.fire();
  await flush();
  assert.equal(reads, 2);
  stop();
});

test("polling skips ticks while the window is hidden and resumes on visibility", async () => {
  const f = fakeEnv();
  let reads = 0;
  const stop = startGitHubPolling("pending", async () => { reads++; }, f.env);
  f.state.hidden = true;
  f.fire();
  f.visible();
  await flush();
  assert.equal(reads, 0);
  f.state.hidden = false;
  f.visible();
  await flush();
  assert.equal(reads, 1);
  stop();
});

test("a slow read is not overlapped by the next tick", async () => {
  const f = fakeEnv();
  let reads = 0;
  let finish: () => void = () => undefined;
  const stop = startGitHubPolling("pending", () => { reads++; return new Promise<void>((r) => { finish = r; }); }, f.env);
  f.fire();
  f.fire();
  await flush();
  assert.equal(reads, 1);
  finish();
  await flush();
  f.fire();
  await flush();
  assert.equal(reads, 2);
  finish();
  stop();
});

test("stopping clears the interval and the visibility listener", () => {
  const f = fakeEnv();
  const stop = startGitHubPolling("pending", async () => undefined, f.env);
  assert.equal(f.state.timers.size, 1);
  assert.equal(f.state.listeners.size, 1);
  stop();
  assert.equal(f.state.cleared, 1);
  assert.equal(f.state.timers.size, 0);
  assert.equal(f.state.listeners.size, 0);
});

test("no timer starts unless the flow is pending", () => {
  for (const state of ["not_connected", "connected", "failed"] as const) {
    const f = fakeEnv();
    startGitHubPolling(state, async () => undefined, f.env)();
    assert.equal(f.state.timers.size, 0, state);
    assert.equal(f.state.listeners.size, 0, state);
  }
});

// The hook cannot be rendered under node --test (no DOM), so its wiring is
// pinned at the source: the effect must hand the poller cleanup back to
// React, otherwise the interval outlives the screen.
test("useGitHub wires the poller and returns its cleanup from the effect", () => {
  const src = readFileSync(new URL("../src/renderer/hooks/useGitHub.ts", import.meta.url), "utf8");
  assert.match(src, /return startGitHubPolling\(/);
  assert.match(src, /browserPollEnv\(\)/);
});

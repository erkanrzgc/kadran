import { test } from "node:test";
import assert from "node:assert/strict";
import { validateManagementResult } from "../src/main/management-results.ts";
test("typed management rejects valid JSON with wrong nested shapes", () => {
  for (const [method, value] of [
    ["apps.list", { apps: null }], ["apps.get", { app: {}, releases: [] }],
    ["apps.logs", { output: null, truncated: false }], ["apps.deploy", { release_id: "r1", image_id: "", output: "", truncated: false }],
    ["alarms.list", { alarms: [null], delivery_is_local_only: true }],
    ["backups.list", { backups: [{ path: "a", bytes: 123, taken_unix: "5" }], keep: 7 }],
    ["backups.create", { backup: null, volumes_excluded: true }],
  ] as const) assert.throws(() => validateManagementResult(method, value), /invalid/);
});
test("typed cancellation passes only in its exact shape", () => {
  const cancelled = { cancelled: true };
  assert.equal(validateManagementResult("apps.deploy", cancelled), cancelled);
  for (const value of [{ cancelled: true, extra: 1 }, { cancelled: false }, { cancelled: "true" }, [], null, {}]) {
    assert.throws(() => validateManagementResult("apps.deploy", value), /invalid/);
  }
});
test("empty resources and bounded textual logs have truthful valid shapes", () => {
  const empty = { apps: [] };
  assert.equal(validateManagementResult("apps.list", empty), empty);
  assert.doesNotThrow(() => validateManagementResult("apps.logs", { output: "İstanbul", truncated: false }));
  assert.doesNotThrow(() => validateManagementResult("backups.list", { backups: [], keep: 7 }));
});

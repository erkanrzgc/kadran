import { test } from "node:test";
import assert from "node:assert/strict";
import { requireAppId, requireAppPayload, requireConfirmation, requireRef, requireTail, createManagementGate } from "../src/main/management-ipc.ts";

test("management IPC rejects ambiguous IDs, credentials, unknown fields and unbounded logs", () => {
  for (const id of ["", "../other", "-other", "App", "a".repeat(33)]) assert.throws(() => requireAppId(id));
  assert.equal(requireAppId("blog-2"), "blog-2");
  assert.throws(() => requireConfirmation("blog", "BLOG"));
  assert.throws(() => requireRef("x\ncmd"));
  assert.throws(() => requireTail(10001));
  assert.equal(requireTail(100), 100);
  assert.throws(() => requireAppPayload({ token: "secret" }, "create"));
  assert.throws(() => requireAppPayload({ container_port: 123 }, "update"));
  assert.throws(() => requireAppPayload(null, "create"));
  assert.throws(() => requireAppPayload({ env: { BIG: "x".repeat(600000) } }, "update"));
  assert.deepEqual(requireAppPayload({ domain: "", env_remove: ["OLD"] }, "update"), { domain: "", env_remove: ["OLD"] });
});

test("management gate prevents duplicate actions and releases after failure", async () => {
  const gate = createManagementGate();
  let finish!: () => void;
  const first = gate("t", "blog", () => new Promise<void>((r) => { finish = r; }));
  await assert.rejects(gate("t", "blog", async () => {}), /already/);
  await gate("t", "other", async () => {});
  finish(); await first;
  await assert.rejects(gate("t", "blog", async () => { throw new Error("failed"); }));
  await gate("t", "blog", async () => {});
});

import assert from "node:assert/strict";
import { test } from "node:test";
import { managementConfirmation, type ConfirmRequest } from "../src/main/management-confirmation.ts";

const REQUESTS: ConfirmRequest[] = [
  { kind: "deploy", target: "user@host", app: "blog", ref: "main" },
  { kind: "rollback", target: "user@host", app: "blog" },
  { kind: "delete", target: "user@host", app: "blog" },
  { kind: "backup", target: "user@host" },
  { kind: "update", target: "user@host", app: "blog", update: { domain: "x.example.com" } },
  { kind: "create", target: "user@host", spec: { app_id: "blog", git_host: "github.com", git_owner: "a", git_repo: "b", git_branch: "main", container_port: 3000 } },
];

test("native confirmation follows the saved language and defaults to cancellation", () => {
  for (const request of REQUESTS) {
    const tr = managementConfirmation("tr", request);
    const en = managementConfirmation("en", request);
    assert.equal(tr.buttons[0], "Vazgeç");
    assert.equal(en.buttons[0], "Cancel");
    assert.ok(tr.detail.startsWith("Sunucu: user@host"));
    assert.ok(en.detail.startsWith("Server: user@host"));
    assert.notEqual(tr.buttons[1], en.buttons[1]);
    assert.equal(tr.defaultId, 0);
    assert.equal(tr.cancelId, 0);
    assert.equal(en.defaultId, 0);
    assert.equal(en.cancelId, 0);
  }
});

test("dialog values cannot re-expand placeholders or hide invisible characters", () => {
  const out = managementConfirmation("en", { kind: "deploy", target: "user@host", app: "blog", ref: "{app}‮" });
  assert.ok(out.detail.includes("Ref: {app}\\u202e"));
  assert.equal(out.detail.includes("‮"), false);
});

test("long lists are truncated with a count instead of growing the dialog", () => {
  const env = Object.fromEntries(Array.from({ length: 30 }, (_, i) => [`KEY_${i}`, "v"]));
  const out = managementConfirmation("en", { kind: "update", target: "t", app: "blog", update: { env } });
  assert.ok(out.detail.includes("and 20 more"));
  assert.equal(out.detail.includes("KEY_29"), false);
});

import assert from "node:assert/strict";
import { test } from "node:test";
import { managementCancellation, managementConfirmation } from "../src/main/management-confirmation.ts";

test("native destructive confirmation follows the saved language and defaults to cancellation", () => {
  for (const method of ["apps.delete", "apps.rollback"] as const) {
    const tr = managementConfirmation("tr", method, "user@host", "blog");
    const en = managementConfirmation("en", method, "user@host", "blog");
    assert.equal(tr.buttons[0], "Vazgeç");
    assert.equal(en.buttons[0], "Cancel");
    assert.ok(tr.detail.startsWith("Sunucu: user@host"));
    assert.ok(en.detail.startsWith("Server: user@host"));
    assert.ok(tr.message.endsWith(": blog"));
    assert.notEqual(tr.buttons[1], en.buttons[1]);
    assert.equal(tr.defaultId, tr.cancelId);
    assert.equal(tr.defaultId, 0);
    assert.notEqual(managementCancellation("tr"), managementCancellation("en"));
  }
});

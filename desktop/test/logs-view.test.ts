import assert from "node:assert/strict";
import { test } from "node:test";

import { logsBody } from "../src/renderer/lib/logs-view.ts";

test("hata ya da yükleme sırasında (veri yok) günlük gövdesi çizilmez", () => {
  assert.deepEqual(logsBody(undefined), { kind: "none" });
  assert.deepEqual(logsBody(null), { kind: "none" });
});

test("veri gelip çıktı boşsa 'boş' denir", () => {
  assert.deepEqual(logsBody({ output: "" }), { kind: "empty" });
});

test("çıktı varsa metin olduğu gibi gösterilir", () => {
  assert.deepEqual(logsBody({ output: "satır\n" }), { kind: "text", text: "satır\n" });
});

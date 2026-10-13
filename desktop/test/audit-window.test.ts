/**
 * Audit sayfalama. Sunucu `after_seq`'i HARİÇ tuyor ve ARTAN sırada
 * dönüyor (internal/store/store.go). Eski arayüz (0, 50) istiyordu ve
 * "son kayıtlar" diye EN ESKİ 50'yi gösteriyordu.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { clampLimit, mergeNewestFirst, newestPageIntact, newestWindow, olderWindow, parseLatestSeq } from "../src/renderer/lib/audit-window.ts";
import type { AuditRecord } from "../src/renderer/lib/types.ts";

function rec(seq: number): AuditRecord {
  return { seq: String(seq), ts: null, actor: null, action: "a", target: "", params_json: "", outcome: "AUDIT_OUTCOME_SUCCESS", detail: "", source: "" };
}

/** Sunucunun davranışı: after_seq'ten SONRAKİLER, artan, en çok limit. */
function serverPage(latest: number, afterSeq: number, limit: number): AuditRecord[] {
  const out: AuditRecord[] = [];
  for (let s = afterSeq + 1; s <= latest && out.length < limit; s++) out.push(rec(s));
  return out;
}

test("en yeni 10: latest 1284 → 1284…1275", () => {
  const w = newestWindow(1284, 10);
  assert.deepEqual(w, { afterSeq: 1274, limit: 10 });
  const shown = mergeNewestFirst([], serverPage(1284, w.afterSeq, w.limit));
  assert.deepEqual(shown.map((r) => r.seq), ["1284", "1283", "1282", "1281", "1280", "1279", "1278", "1277", "1276", "1275"]);
});

test("kayıt sayısı pencereden azsa baştan", () => {
  assert.deepEqual(newestWindow(5, 50), { afterSeq: 0, limit: 50 });
});

test("daha eskiler: ekrandaki en eskinin hemen öncesi", () => {
  assert.deepEqual(olderWindow(1235, 50), { afterSeq: 1184, limit: 50 });
  const page = serverPage(1284, 1184, 50);
  assert.equal(page[0]!.seq, "1185");
  assert.equal(page[page.length - 1]!.seq, "1234");
  assert.deepEqual(olderWindow(30, 50), { afterSeq: 0, limit: 29 });
  assert.equal(olderWindow(1, 50), null);
  assert.equal(olderWindow(0, 50), null);
});

test("birleştirme tekrar etmez ve en yeni üstte", () => {
  const merged = mergeNewestFirst([rec(5), rec(4)], [rec(3), rec(4)]);
  assert.deepEqual(merged.map((r) => r.seq), ["5", "4", "3"]);
});

test("limit 1…1000 aralığında", () => {
  assert.equal(clampLimit(0), 1);
  assert.equal(clampLimit(5000), 1000);
  assert.equal(clampLimit(Number.NaN), 1);
  assert.equal(clampLimit(50.7), 50);
});

test("latest_seq yalnız ondalık rakam dizisi; boş zincir tam \"0\"", () => {
  assert.equal(parseLatestSeq("0"), 0);
  assert.equal(parseLatestSeq("1284"), 1284);
});

test("en yeni sayfa latest_seq kaydını içermeli: boş ya da eksik sayfa \"kayıt yok\" değil hata", () => {
  assert.equal(newestPageIntact(1284, serverPage(1284, 1274, 10)), true);
  assert.equal(newestPageIntact(5, serverPage(5, 0, 50)), true);
  // İki çağrı arasında yeni kayıt geldiyse de pencere latest'i içerir.
  assert.equal(newestPageIntact(1284, serverPage(1290, 1274, 10)), true);
  assert.equal(newestPageIntact(1284, []), false);
  assert.equal(newestPageIntact(1284, serverPage(1283, 1273, 10)), false);
});

test("latest_seq bozuksa null: \"kayıt yok\" diye okunmaz", () => {
  for (const bad of [null, undefined, "", " 1", "abc", "-1", "1.5", "1e3", "0x10", "01", 12, "9007199254740993"]) {
    assert.equal(parseLatestSeq(bad), null, String(bad));
  }
});

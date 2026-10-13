/**
 * Kayıt tablosunun boş satırı. Yüklenemeyen liste "kayıt yok" diye
 * gösterilmez; süzgeç her şeyi elediyse bu "zincir boş" değildir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { auditPlaceholderKey, effectiveChoice } from "../src/renderer/lib/audit-view.ts";

const base = { total: 0, shown: 0, failed: false, loaded: false };

test("kayıt varken boş satır yok; süzgeç hepsini elediyse no_match", () => {
  assert.equal(auditPlaceholderKey({ ...base, total: 5, shown: 5, loaded: true }), undefined);
  assert.equal(auditPlaceholderKey({ ...base, total: 5, shown: 0, loaded: true }), "audit.no_match");
});

test("liste boşken: hata > yüklendi > yükleniyor", () => {
  assert.equal(auditPlaceholderKey({ ...base, failed: true, loaded: true }), "audit.load_failed");
  assert.equal(auditPlaceholderKey({ ...base, loaded: true }), "audit.empty");
  assert.equal(auditPlaceholderKey(base), "audit.loading");
});

test("seçim listede yoksa 'tümü' gibi davranır: seçici ile süzgeç aynı şeyi söyler", () => {
  assert.equal(effectiveChoice("deploy", ["backup", "deploy"]), "deploy");
  assert.equal(effectiveChoice("deploy", ["backup"]), "");
  assert.equal(effectiveChoice("", ["backup"]), "");
});

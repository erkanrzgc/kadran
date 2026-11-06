/**
 * Windows camı. Çağrının kendisi gerçek pencerede ölçüldü (K-161) ve duman
 * yolculuğu Windows'ta yeniden deniyor; burada gönderilen değerler sınanır.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { accentPolicy, handleValue } from "../src/main/window-accent.ts";

test("cam açıkken Akrilik, ton temaya göre (K-163); kapalıyken efekt yok", () => {
  // Kullanıcı Electron deneme penceresinde seçti (6 Kasım): açıkta A3, koyuda K1.
  // Düz bulanıklık (3) Electron'un penceresinde masaüstünü göstermedi, arka siyah kaldı.
  assert.deepEqual(accentPolicy(true, "light"), { State: 4, Flags: 2, Color: 0xa0f7f4f4, AnimationId: 0 });
  assert.deepEqual(accentPolicy(true, "dark"), { State: 4, Flags: 2, Color: 0x30181816, AnimationId: 0 });
  for (const scheme of ["light", "dark"] as const) {
    assert.ok(accentPolicy(true, scheme).Color >>> 24 > 0, "ton sıfır saydamlıkta pencere simsiyah çizilir");
    assert.equal(accentPolicy(false, scheme).State, 0);
  }
});

test("pencere kimliği iki genişlikte de okunur", () => {
  const wide = Buffer.alloc(8);
  wide.writeBigUInt64LE(0x20a7cn);
  assert.equal(handleValue(wide), 0x20a7c);
  const narrow = Buffer.alloc(4);
  narrow.writeUInt32LE(0x20a7c);
  assert.equal(handleValue(narrow), 0x20a7c);
});

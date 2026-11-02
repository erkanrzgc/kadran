/**
 * Windows camı. Çağrının kendisi gerçek pencerede ölçüldü (K-161) ve duman
 * yolculuğu Windows'ta yeniden deniyor; burada gönderilen değerler sınanır.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { accentPolicy, handleValue } from "../src/main/window-accent.ts";

test("cam açıkken Acrylic bulanıklık ve ton, kapalıyken efekt yok", () => {
  const on = accentPolicy(true);
  assert.equal(on.State, 4);
  assert.equal(on.Flags, 2, "ton bayrağı yoksa renk yok sayılır");
  assert.ok(on.Color >>> 24 > 0, "ton sıfır saydamlıkta pencere simsiyah çizilir");
  assert.equal(accentPolicy(false).State, 0);
});

test("pencere kimliği iki genişlikte de okunur", () => {
  const wide = Buffer.alloc(8);
  wide.writeBigUInt64LE(0x20a7cn);
  assert.equal(handleValue(wide), 0x20a7c);
  const narrow = Buffer.alloc(4);
  narrow.writeUInt32LE(0x20a7c);
  assert.equal(handleValue(narrow), 0x20a7c);
});

/**
 * Kenar çubuğunun kayan göstergesi (kullanıcının seçtiği 7, K-163): seçili
 * satırın camı ve fareyi izleyen vurgu, satırın kap içindeki yerine oturur.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { slideBox } from "../src/renderer/lib/nav-slider.ts";

const BOX = { top: 100, left: 20, width: 200, height: 600 };
const ITEM = { top: 180, left: 26, width: 188, height: 36 };
const NO_BORDER = { top: 0, left: 0 };
const NO_SCROLL = { top: 0, left: 0 };

test("gösterge satırın kap içindeki yerine ve boyuna oturur", () => {
  assert.deepEqual(slideBox(BOX, NO_BORDER, ITEM, NO_SCROLL), { x: 6, y: 80, width: 188, height: 36 });
});

test("kabın kenarlığı düşülür: mutlak konumlu gösterge dolgu kutusuna göre durur (panel 1 px kenarlı)", () => {
  assert.deepEqual(slideBox(BOX, { top: 1, left: 1 }, ITEM, NO_SCROLL), { x: 5, y: 79, width: 188, height: 36 });
});

test("kap kaydırılmışsa gösterge içerikle birlikte kayar", () => {
  assert.deepEqual(slideBox(BOX, NO_BORDER, ITEM, { top: 120, left: 0 }), { x: 6, y: 200, width: 188, height: 36 });
  // Dar pencerede ikinci menü yatay kayar.
  assert.deepEqual(slideBox(BOX, NO_BORDER, ITEM, { top: 0, left: 40 }), { x: 46, y: 80, width: 188, height: 36 });
});

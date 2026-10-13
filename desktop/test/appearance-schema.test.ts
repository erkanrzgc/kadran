/**
 * Görünüm ayarlarının şeması. Dosya elle düzenlenebilir ya da bozulabilir:
 * her alan doğrulanır, geçersiz olan varsayılana döner, program çökmez.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  defaultAppearance,
  isWindows11,
  modesFor,
  sanitizeAppearance,
  type PlatformInfo,
} from "../src/shared/appearance.ts";

const WIN11: PlatformInfo = { platform: "win32", release: "10.0.26200" };
const WIN10: PlatformInfo = { platform: "win32", release: "10.0.19045" };
const MAC: PlatformInfo = { platform: "darwin", release: "24.6.0" };
const LINUX: PlatformInfo = { platform: "linux", release: "6.12.48" };

test("Windows 11 ayrımı yapı numarasından (22H2 = 22621)", () => {
  assert.equal(isWindows11(WIN11), true);
  assert.equal(isWindows11({ platform: "win32", release: "10.0.22621" }), true);
  assert.equal(isWindows11({ platform: "win32", release: "10.0.22000" }), false);
  assert.equal(isWindows11(WIN10), false);
  assert.equal(isWindows11(MAC), false);
});

test("her sistemde sunulan modlar ve varsayılan", () => {
  assert.deepEqual(modesFor(WIN11), ["clear", "acrylic", "mica", "solid"]);
  assert.deepEqual(modesFor(WIN10), ["clear", "solid"]);
  assert.deepEqual(modesFor(MAC), ["clear", "vibrancy", "solid"]);
  assert.deepEqual(modesFor(LINUX), ["clear", "solid"]);
  assert.equal(defaultAppearance(WIN11).mode, "acrylic");
  assert.equal(defaultAppearance(WIN10).mode, "clear");
  assert.equal(defaultAppearance(MAC).mode, "vibrancy");
  assert.equal(defaultAppearance(LINUX).mode, "solid");
});

test("varsayılan değerler spec ile aynı", () => {
  const d = defaultAppearance(WIN11);
  assert.deepEqual(d, {
    version: 1,
    mode: "acrylic",
    tint: 60,
    panel: 7,
    edge: 22,
    glow: 60,
    accent: "ice",
    tone: "dark",
    language: "en",
    uiFont: { family: "", weight: 400 },
    codeFont: { family: "", weight: 400 },
  });
});

test("nesne değilse ya da diziyse varsayılan", () => {
  for (const raw of [null, undefined, 42, "x", [], [1, 2]]) {
    assert.deepEqual(sanitizeAppearance(raw, WIN11), defaultAppearance(WIN11));
  }
});

test("sınır dışı sayılar kırpılır, sayı olmayanlar varsayılana döner", () => {
  const a = sanitizeAppearance({ tint: 150, panel: -4, edge: 61.6, glow: "90" }, WIN11);
  assert.equal(a.tint, 100);
  assert.equal(a.panel, 0);
  assert.equal(a.edge, 60);
  assert.equal(a.glow, 60);
  assert.equal(sanitizeAppearance({ tint: Number.NaN }, WIN11).tint, 60);
  assert.equal(sanitizeAppearance({ tint: 33.4 }, WIN11).tint, 33);
});

test("bu sistemde olmayan mod varsayılana döner", () => {
  assert.equal(sanitizeAppearance({ mode: "acrylic" }, WIN10).mode, "clear");
  assert.equal(sanitizeAppearance({ mode: "vibrancy" }, WIN11).mode, "acrylic");
  assert.equal(sanitizeAppearance({ mode: "mica" }, WIN11).mode, "mica");
  assert.equal(sanitizeAppearance({ mode: "holographic" }, WIN11).mode, "acrylic");
});

test("bilinmeyen seçimler varsayılana döner", () => {
  const a = sanitizeAppearance({ accent: "neon", tone: "sepia", language: "de" }, WIN11);
  assert.equal(a.accent, "ice");
  assert.equal(a.tone, "dark");
  assert.equal(a.language, "en");
  assert.equal(sanitizeAppearance({ language: "tr" }, WIN11).language, "tr");
});

test("yazı tipi: uzun ad reddedilir, kalınlık 100–900", () => {
  const long = "x".repeat(10_000);
  const a = sanitizeAppearance({ uiFont: { family: long, weight: 500 }, codeFont: { family: "JetBrainsMono Nerd Font", weight: 1200 } }, WIN11);
  assert.deepEqual(a.uiFont, { family: "", weight: 500 });
  assert.deepEqual(a.codeFont, { family: "JetBrainsMono Nerd Font", weight: 900 });
  assert.deepEqual(sanitizeAppearance({ uiFont: "Arial" }, WIN11).uiFont, { family: "", weight: 400 });
});

test("geçerli kısmi nesne kendi alanlarını korur", () => {
  const a = sanitizeAppearance({ tint: 48, panel: 6, edge: 8, mode: "clear" }, WIN11);
  assert.equal(a.tint, 48);
  assert.equal(a.panel, 6);
  assert.equal(a.edge, 8);
  assert.equal(a.mode, "clear");
  assert.equal(a.version, 1);
});

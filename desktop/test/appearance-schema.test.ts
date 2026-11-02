/**
 * Ayarların şeması. Dosya elle düzenlenebilir ya da bozulabilir: her alan
 * doğrulanır, geçersiz olan varsayılana döner, program çökmez.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { APPEARANCE_VERSION, defaultAppearance, effectiveTheme, isWindows11, materialFor, sanitizeAppearance, windowLook, type PlatformInfo } from "../src/shared/appearance.ts";

const WIN11: PlatformInfo = { platform: "win32", release: "10.0.26200" };
const WIN10: PlatformInfo = { platform: "win32", release: "10.0.19045" };
const MAC: PlatformInfo = { platform: "darwin", release: "24.6.0" };
const LINUX: PlatformInfo = { platform: "linux", release: "6.12.48" };
const V = { version: APPEARANCE_VERSION };

test("Windows 11 ayrımı yapı numarasından (22H2 = 22621)", () => {
  assert.equal(isWindows11(WIN11), true);
  assert.equal(isWindows11({ platform: "win32", release: "10.0.22621" }), true);
  assert.equal(isWindows11({ platform: "win32", release: "10.0.22000" }), false);
  assert.equal(isWindows11(WIN10), false);
  assert.equal(isWindows11(MAC), false);
});

test("yerel malzeme: desteklemeyen sistem opak düşer", () => {
  assert.equal(materialFor(WIN11), "acrylic");
  assert.equal(materialFor(WIN10), "none");
  assert.equal(materialFor(MAC), "vibrancy");
  assert.equal(materialFor(LINUX), "none");
});

test("varsayılan değerler", () => {
  assert.deepEqual(defaultAppearance(), { version: 2, language: "en", theme: "glass", uiFont: { family: "", weight: 400 }, codeFont: { family: "", weight: 400 } });
});

test("tema: bilinmeyen değer varsayılana döner, tema alanı olmayan dosyada yazı tipi korunur", () => {
  assert.equal(sanitizeAppearance({ ...V, theme: "light" }).theme, "light");
  assert.equal(sanitizeAppearance({ ...V, theme: "dark" }).theme, "dark");
  assert.equal(sanitizeAppearance({ ...V, theme: "mica" }).theme, "glass");
  assert.equal(sanitizeAppearance({ ...V, theme: "__proto__" }).theme, "glass");
  const saved = sanitizeAppearance({ ...V, uiFont: { family: "Arial", weight: 500 } });
  assert.equal(saved.theme, "glass");
  assert.deepEqual(saved.uiFont, { family: "Arial", weight: 500 });
});

test("cam desteklenmeyen sistemde koyuya düşer", () => {
  assert.equal(effectiveTheme("glass", "none"), "dark");
  assert.equal(effectiveTheme("glass", "accent"), "glass");
  assert.equal(effectiveTheme("glass", "acrylic"), "glass");
  assert.equal(effectiveTheme("glass", "vibrancy"), "glass");
  assert.equal(effectiveTheme("light", "none"), "light");
  assert.equal(effectiveTheme("dark", "acrylic"), "dark");
});

test("tema → pencere: yalnız cam saydam; koyu ve açık opak", () => {
  assert.deepEqual(windowLook("glass", "accent"), { material: "accent", background: "#00000000", scheme: "dark" });
  assert.deepEqual(windowLook("dark", "accent"), { material: "none", background: "#0a0a0a", scheme: "dark" });
  assert.deepEqual(windowLook("glass", "acrylic"), { material: "acrylic", background: "#00000000", scheme: "dark" });
  assert.deepEqual(windowLook("glass", "vibrancy"), { material: "vibrancy", background: "#00000000", scheme: "dark" });
  assert.deepEqual(windowLook("glass", "none"), { material: "none", background: "#0a0a0a", scheme: "dark" });
  assert.deepEqual(windowLook("dark", "acrylic"), { material: "none", background: "#0a0a0a", scheme: "dark" });
  assert.deepEqual(windowLook("light", "acrylic"), { material: "none", background: "#fafafa", scheme: "light" });
  assert.deepEqual(windowLook("light", "none"), { material: "none", background: "#fafafa", scheme: "light" });
});

test("nesne değilse ya da diziyse varsayılan", () => {
  for (const raw of [null, undefined, 42, "x", [], [1, 2]]) {
    assert.deepEqual(sanitizeAppearance(raw), defaultAppearance());
  }
});

test("eski dosya: dil taşınır, görünüm ayarları ve eski yazı tipi atılır", () => {
  const old = { version: 1, mode: "clear", tint: 96, panel: 0, accent: "amber", tone: "light", language: "tr", uiFont: { family: "JetBrainsMono Nerd Font Mono", weight: 500 } };
  assert.deepEqual(sanitizeAppearance(old), { ...defaultAppearance(), language: "tr" });
});

test("bilinmeyen dil varsayılana döner", () => {
  assert.equal(sanitizeAppearance({ ...V, language: "de" }).language, "en");
  assert.equal(sanitizeAppearance({ ...V, language: "tr" }).language, "tr");
});

test("yazı tipi: uzun ad reddedilir, kalınlık 100–900", () => {
  const long = "x".repeat(10_000);
  const a = sanitizeAppearance({ ...V, uiFont: { family: long, weight: 500 }, codeFont: { family: "JetBrainsMono Nerd Font", weight: 1200 } });
  assert.deepEqual(a.uiFont, { family: "", weight: 500 });
  assert.deepEqual(a.codeFont, { family: "JetBrainsMono Nerd Font", weight: 900 });
  assert.deepEqual(sanitizeAppearance({ ...V, uiFont: "Arial" }).uiFont, { family: "", weight: 400 });
});

test("bilinmeyen alan diske geçmez", () => {
  assert.equal("mode" in sanitizeAppearance({ ...V, mode: "mica", extra: 1 }), false);
});

/**
 * Pencere seçenekleri: güvenlik ayarları her görünümde aynı; görünüm yalnız
 * arkada ne görüneceğini değiştirir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { windowOptions } from "../src/main/window-options.ts";
import { windowLook, type Material, type Theme } from "../src/shared/appearance.ts";

const PRELOAD = "/out/preload/index.cjs";
const THEMES: readonly Theme[] = ["dark", "light", "glass"];
const MATERIALS: readonly Material[] = ["accent", "acrylic", "vibrancy", "none"];

test("her görünümde güvenlik ayarları aynı ve sıkı", () => {
  for (const theme of THEMES) {
    for (const material of MATERIALS) {
      for (const platform of ["win32", "darwin", "linux"]) {
        const o = windowOptions(windowLook(theme, material), platform, PRELOAD);
        assert.deepEqual(
          o.webPreferences,
          { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true, webviewTag: false },
          `${theme}/${material}/${platform}`,
        );
        assert.equal(o.show, false, "hazır olmadan gösterilmemeli");
        assert.equal(o.transparent, undefined, "saydam pencere masaüstünü bulanıklaştıramaz");
      }
    }
  }
});

test("görünüm → pencere", () => {
  // Windows camı pencere açıldıktan sonra uygulanır; Electron'un malzemesi onu siyaha boyar.
  const accent = windowOptions(windowLook("glass", "accent"), "win32", PRELOAD);
  assert.equal(accent.backgroundMaterial, undefined);
  assert.equal(accent.backgroundColor, "#00000000");
  assert.equal(accent.transparent, undefined, "saydam pencere kenardan boyutlanmaz, köşesi yuvarlak olmaz");
  assert.equal(accent.frame, false);

  const acrylic = windowOptions(windowLook("glass", "acrylic"), "win32", PRELOAD);
  assert.equal(acrylic.backgroundMaterial, "acrylic");
  assert.equal(acrylic.backgroundColor, "#00000000");
  assert.equal(acrylic.frame, false);

  const vib = windowOptions(windowLook("glass", "vibrancy"), "darwin", PRELOAD);
  assert.equal(vib.vibrancy, "under-window");
  assert.equal(vib.titleBarStyle, "hiddenInset");
  assert.equal(vib.frame, undefined, "macOS'ta trafik ışıkları kalsın");

  const light = windowOptions(windowLook("light", "acrylic"), "win32", PRELOAD);
  assert.equal(light.backgroundColor, "#fafafa");
  assert.equal(light.backgroundMaterial, undefined);

  const dark = windowOptions(windowLook("dark", "none"), "linux", PRELOAD);
  assert.equal(dark.backgroundColor, "#0a0a0a");
  assert.equal(dark.backgroundMaterial, undefined);
});

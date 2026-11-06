/**
 * Pencere seçenekleri: güvenlik ayarları her görünümde aynı; görünüm yalnız
 * arkada ne görüneceğini değiştirir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { lookChange, windowOptions } from "../src/main/window-options.ts";
import { windowLook, type Material, type Theme } from "../src/shared/appearance.ts";

const PRELOAD = "/out/preload/index.cjs";
const THEMES: readonly Theme[] = ["dark", "light"];
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
  // Cam her temada (K-163): açık ve koyu temada da pencere saydam açılır.
  for (const theme of THEMES) {
    const accent = windowOptions(windowLook(theme, "accent"), "win32", PRELOAD);
    assert.equal(accent.backgroundMaterial, undefined, theme);
    assert.equal(accent.backgroundColor, "#00000000", theme);
    assert.equal(accent.transparent, undefined, "saydam pencere kenardan boyutlanmaz, köşesi yuvarlak olmaz");
    assert.equal(accent.frame, false);

    const acrylic = windowOptions(windowLook(theme, "acrylic"), "win32", PRELOAD);
    assert.equal(acrylic.backgroundMaterial, "acrylic", theme);
    assert.equal(acrylic.backgroundColor, "#00000000", theme);
    assert.equal(acrylic.frame, false);

    const vib = windowOptions(windowLook(theme, "vibrancy"), "darwin", PRELOAD);
    assert.equal(vib.vibrancy, "under-window", theme);
    assert.equal(vib.titleBarStyle, "hiddenInset");
    assert.equal(vib.frame, undefined, "macOS'ta trafik ışıkları kalsın");
  }

  const light = windowOptions(windowLook("light", "none"), "linux", PRELOAD);
  assert.equal(light.backgroundColor, "#ededf0");
  assert.equal(light.backgroundMaterial, undefined);

  const dark = windowOptions(windowLook("dark", "none"), "linux", PRELOAD);
  assert.equal(dark.backgroundColor, "#0a0a0a");
  assert.equal(dark.backgroundMaterial, undefined);
});

test("tema değişince: malzeme yeniden kurulmaz; yalnız opak zeminin rengi ve Windows camının tonu değişir", () => {
  const accentDark = windowLook("dark", "accent");
  const accentLight = windowLook("light", "accent");
  assert.deepEqual(lookChange(accentDark, accentLight), { background: null, accent: "light" });
  assert.deepEqual(lookChange(accentLight, accentDark), { background: null, accent: "dark" });
  // Yazı tipi ya da dil kaydı da buradan geçer: aynı görünüm hiçbir şeyi yeniden kurmaz (titreme).
  assert.deepEqual(lookChange(accentDark, windowLook("dark", "accent")), { background: null, accent: null });
  assert.deepEqual(lookChange(windowLook("dark", "none"), windowLook("light", "none")), { background: "#ededf0", accent: null });
  assert.deepEqual(lookChange(windowLook("light", "none"), windowLook("light", "none")), { background: null, accent: null });
  // Yedek Acrylic'in ve macOS'un tonunu sistem teması (nativeTheme) verir.
  assert.deepEqual(lookChange(windowLook("dark", "acrylic"), windowLook("light", "acrylic")), { background: null, accent: null });
  assert.deepEqual(lookChange(windowLook("dark", "vibrancy"), windowLook("light", "vibrancy")), { background: null, accent: null });
});

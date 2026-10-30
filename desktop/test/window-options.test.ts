/**
 * Pencere seçenekleri: güvenlik ayarları HER modda aynı; mod yalnız
 * görünüşü değiştirir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { ALL_MODES } from "../src/shared/appearance.ts";
import { GLASS_BACKGROUND, needsRecreate, SOLID_BACKGROUND, windowOptions } from "../src/main/window-options.ts";

const PRELOAD = "/out/preload/index.cjs";

test("her modda güvenlik ayarları aynı ve sıkı", () => {
  for (const mode of ALL_MODES) {
    for (const platform of ["win32", "darwin", "linux"]) {
      const o = windowOptions(mode, platform, PRELOAD);
      assert.deepEqual(
        o.webPreferences,
        { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true, webviewTag: false },
        `${mode}/${platform}`,
      );
      assert.equal(o.show, false, "hazır olmadan gösterilmemeli");
      assert.equal(o.transparent, undefined, "saydam pencere masaüstünü bulanıklaştıramaz");
    }
  }
});

test("mod → pencere malzemesi", () => {
  const glass = windowOptions("glass", "win32", PRELOAD);
  assert.equal(glass.backgroundColor, GLASS_BACKGROUND);
  assert.equal(glass.frame, false);
  assert.equal(glass.backgroundMaterial, undefined);

  assert.equal(windowOptions("acrylic", "win32", PRELOAD).backgroundMaterial, "acrylic");
  assert.equal(windowOptions("mica", "win32", PRELOAD).backgroundMaterial, "mica");

  const vib = windowOptions("vibrancy", "darwin", PRELOAD);
  assert.equal(vib.vibrancy, "under-window");
  assert.equal(vib.titleBarStyle, "hiddenInset");
  assert.equal(vib.frame, undefined, "macOS'ta trafik ışıkları kalsın");

  const solid = windowOptions("solid", "linux", PRELOAD);
  assert.equal(solid.backgroundColor, SOLID_BACKGROUND);
});

test("önceki durum taşınır", () => {
  const o = windowOptions("glass", "win32", PRELOAD, { bounds: { x: 10, y: 20, width: 900, height: 600 }, maximized: true });
  assert.equal(o.x, 10);
  assert.equal(o.y, 20);
  assert.equal(o.width, 900);
  assert.equal(o.height, 600);
});

test("aynı malzeme ailesinde pencere yeniden kurulmaz", () => {
  assert.equal(needsRecreate("acrylic", "mica"), false);
  assert.equal(needsRecreate("mica", "acrylic"), false);
  assert.equal(needsRecreate("mica", "mica"), false);
  assert.equal(needsRecreate("glass", "solid"), false);
  assert.equal(needsRecreate("solid", "glass"), false);
  assert.equal(needsRecreate("glass", "acrylic"), true);
  assert.equal(needsRecreate("acrylic", "glass"), true);
  assert.equal(needsRecreate("solid", "mica"), true);
  assert.equal(needsRecreate("vibrancy", "solid"), true);
});

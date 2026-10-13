/**
 * Pencere seçenekleri: güvenlik ayarları HER modda aynı; mod yalnız
 * görünüşü değiştirir. Saydamlık pencere kurulduktan sonra açılıp
 * kapanamaz, bu yüzden Clear ile diğerleri arasında pencere yeniden kurulur.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { ALL_MODES } from "../src/shared/appearance.ts";
import { needsRecreate, SOLID_BACKGROUND, windowOptions } from "../src/main/window-options.ts";

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
    }
  }
});

test("mod → pencere malzemesi", () => {
  const clear = windowOptions("clear", "win32", PRELOAD);
  assert.equal(clear.transparent, true);
  assert.equal(clear.backgroundColor, "#00000000");
  assert.equal(clear.frame, false);

  assert.equal(windowOptions("acrylic", "win32", PRELOAD).backgroundMaterial, "acrylic");
  assert.equal(windowOptions("mica", "win32", PRELOAD).backgroundMaterial, "mica");
  assert.equal(windowOptions("acrylic", "win32", PRELOAD).transparent, undefined);

  const vib = windowOptions("vibrancy", "darwin", PRELOAD);
  assert.equal(vib.vibrancy, "under-window");
  assert.equal(vib.titleBarStyle, "hiddenInset");
  assert.equal(vib.frame, undefined, "macOS'ta trafik ışıkları kalsın");

  const solid = windowOptions("solid", "linux", PRELOAD);
  assert.equal(solid.backgroundColor, SOLID_BACKGROUND);
  assert.equal(solid.transparent, undefined);
});

test("önceki durum taşınır", () => {
  const o = windowOptions("clear", "win32", PRELOAD, { bounds: { x: 10, y: 20, width: 900, height: 600 }, maximized: true });
  assert.equal(o.x, 10);
  assert.equal(o.y, 20);
  assert.equal(o.width, 900);
  assert.equal(o.height, 600);
});

test("yalnız Acrylic ↔ Mica yerinde değişir", () => {
  assert.equal(needsRecreate("acrylic", "mica"), false);
  assert.equal(needsRecreate("mica", "acrylic"), false);
  assert.equal(needsRecreate("mica", "mica"), false);
  assert.equal(needsRecreate("clear", "acrylic"), true);
  assert.equal(needsRecreate("acrylic", "clear"), true);
  assert.equal(needsRecreate("solid", "mica"), true);
  assert.equal(needsRecreate("vibrancy", "solid"), true);
});

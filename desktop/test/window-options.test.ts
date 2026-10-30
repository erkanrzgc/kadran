/**
 * Pencere seçenekleri: güvenlik ayarları her malzemede aynı; malzeme yalnız
 * arkada ne görüneceğini değiştirir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { OPAQUE_BACKGROUND, windowOptions } from "../src/main/window-options.ts";
import type { Material } from "../src/shared/appearance.ts";

const PRELOAD = "/out/preload/index.cjs";
const MATERIALS: readonly Material[] = ["acrylic", "vibrancy", "none"];

test("her malzemede güvenlik ayarları aynı ve sıkı", () => {
  for (const material of MATERIALS) {
    for (const platform of ["win32", "darwin", "linux"]) {
      const o = windowOptions(material, platform, PRELOAD);
      assert.deepEqual(
        o.webPreferences,
        { preload: PRELOAD, contextIsolation: true, nodeIntegration: false, sandbox: true, webviewTag: false },
        `${material}/${platform}`,
      );
      assert.equal(o.show, false, "hazır olmadan gösterilmemeli");
      assert.equal(o.transparent, undefined, "saydam pencere masaüstünü bulanıklaştıramaz");
    }
  }
});

test("malzeme → pencere", () => {
  const acrylic = windowOptions("acrylic", "win32", PRELOAD);
  assert.equal(acrylic.backgroundMaterial, "acrylic");
  assert.equal(acrylic.backgroundColor, "#00000000");
  assert.equal(acrylic.frame, false);

  const vib = windowOptions("vibrancy", "darwin", PRELOAD);
  assert.equal(vib.vibrancy, "under-window");
  assert.equal(vib.titleBarStyle, "hiddenInset");
  assert.equal(vib.frame, undefined, "macOS'ta trafik ışıkları kalsın");

  const none = windowOptions("none", "linux", PRELOAD);
  assert.equal(none.backgroundColor, OPAQUE_BACKGROUND);
  assert.equal(none.backgroundMaterial, undefined);
});

/**
 * Malzeme → BrowserWindow seçenekleri. SAF: Electron'u yalnız tip olarak
 * içe aktarır, böylece node --test ile ölçülür.
 *
 * Güvenlik ayarları (webPreferences) HER malzemede aynıdır. Üçü birlikte
 * renderer'ı Node'dan tamamen ayırır; Electron'un varsayılanları olsalar
 * da AÇIKÇA yazılıyorlar: varsayılana güvenmek bir sürüm yükseltmesinde
 * sessizce değişebilir.
 */

import type { BrowserWindowConstructorOptions } from "electron";

import type { Material } from "../shared/appearance.ts";

export const OPAQUE_BACKGROUND = "#16161a";
const CLEAR = "#00000000";

export function windowOptions(material: Material, platform: string, preloadPath: string): BrowserWindowConstructorOptions {
  const base: BrowserWindowConstructorOptions = {
    width: 1280,
    height: 820,
    minWidth: 760,
    minHeight: 520,
    title: "KΛDRΛN",
    show: false,
    webPreferences: {
      preload: preloadPath,
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      webviewTag: false,
    },
  };
  // Başlık çubuğunu biz çiziyoruz; macOS'ta sistem trafik ışıkları kalır.
  const chrome: BrowserWindowConstructorOptions =
    platform === "darwin" ? { titleBarStyle: "hiddenInset" } : { frame: false };

  switch (material) {
    case "acrylic":
      return { ...base, ...chrome, backgroundColor: CLEAR, backgroundMaterial: "acrylic" };
    case "vibrancy":
      return { ...base, ...chrome, backgroundColor: CLEAR, vibrancy: "under-window", visualEffectState: "active" };
    case "none":
      return { ...base, ...chrome, backgroundColor: OPAQUE_BACKGROUND };
  }
}

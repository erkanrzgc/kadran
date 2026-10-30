/**
 * Mod → BrowserWindow seçenekleri. SAF: Electron'u yalnız tip olarak
 * içe aktarır, böylece node --test ile ölçülür.
 *
 * Güvenlik ayarları (webPreferences) HER modda aynıdır. Üçü birlikte
 * renderer'ı Node'dan tamamen ayırır; Electron'un varsayılanları olsalar
 * da AÇIKÇA yazılıyorlar: varsayılana güvenmek bir sürüm yükseltmesinde
 * sessizce değişebilir.
 */

import type { BrowserWindowConstructorOptions, Rectangle } from "electron";

import type { WindowMode } from "../shared/appearance.ts";

export const SOLID_BACKGROUND = "#0e1016";
/** Renkli camın zemini yüklenene kadar görünen renk. */
export const GLASS_BACKGROUND = "#0e1422";
const CLEAR = "#00000000";

export interface WindowState {
  bounds?: Rectangle;
  maximized: boolean;
}

export function windowOptions(
  mode: WindowMode,
  platform: string,
  preloadPath: string,
  state?: WindowState,
): BrowserWindowConstructorOptions {
  const base: BrowserWindowConstructorOptions = {
    width: 1180,
    height: 780,
    minWidth: 760,
    minHeight: 520,
    title: "KΛDRΛN",
    show: false,
    ...(state?.bounds ?? {}),
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

  switch (mode) {
    case "glass":
      return { ...base, ...chrome, backgroundColor: GLASS_BACKGROUND };
    case "acrylic":
    case "mica":
      return { ...base, ...chrome, backgroundColor: CLEAR, backgroundMaterial: mode };
    case "vibrancy":
      return { ...base, ...chrome, backgroundColor: CLEAR, vibrancy: "under-window", visualEffectState: "active" };
    case "solid":
      return { ...base, ...chrome, backgroundColor: SOLID_BACKGROUND };
  }
}

/** needsRecreate: Acrylic ↔ Mica yerinde değişir; Renkli cam ↔ Solid yalnız CSS'tir. */
export function needsRecreate(from: WindowMode, to: WindowMode): boolean {
  if (from === to) return false;
  const live = (m: WindowMode) => m === "acrylic" || m === "mica";
  const opaque = (m: WindowMode) => m === "glass" || m === "solid";
  return !((live(from) && live(to)) || (opaque(from) && opaque(to)));
}

/**
 * Görünüm → BrowserWindow seçenekleri. SAF: Electron'u yalnız tip olarak
 * içe aktarır, böylece node --test ile ölçülür.
 *
 * Güvenlik ayarları (webPreferences) HER görünümde aynıdır. Üçü birlikte
 * renderer'ı Node'dan tamamen ayırır; Electron'un varsayılanları olsalar
 * da AÇIKÇA yazılıyorlar: varsayılana güvenmek bir sürüm yükseltmesinde
 * sessizce değişebilir.
 */

import type { BrowserWindowConstructorOptions } from "electron";

import type { Theme, WindowLook } from "../shared/appearance.ts";

/** Tema ya da ayar kaydı değişince pencereye yeniden verilecekler; null: dokunma. */
export interface LookChange {
  background: string | null;
  /** Windows camının yeni tonu (tema başına, K-163). */
  accent: Theme | null;
}

/**
 * Malzeme sistemin, temadan bağımsız: pencere açılırken bir kez kurulur. Tema
 * yalnız malzemesiz sistemde opak zeminin rengini ve Windows camının tonunu
 * değiştirir. Yedek Acrylic'in ve macOS'un tonunu sistem teması (nativeTheme)
 * verir. Yazı tipi ya da dil kaydı da buradan geçer; aynısını yeniden vermek titretir.
 */
export function lookChange(prev: WindowLook, next: WindowLook): LookChange {
  return {
    background: next.background !== prev.background ? next.background : null,
    accent: next.material === "accent" && next.scheme !== prev.scheme ? next.scheme : null,
  };
}

export function windowOptions(look: WindowLook, platform: string, preloadPath: string): BrowserWindowConstructorOptions {
  const base: BrowserWindowConstructorOptions = {
    width: 1280,
    height: 820,
    minWidth: 760,
    minHeight: 520,
    title: "KΛDRΛN",
    show: false,
    backgroundColor: look.background,
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

  switch (look.material) {
    case "accent":
      return { ...base, ...chrome };
    case "acrylic":
      return { ...base, ...chrome, backgroundMaterial: "acrylic" };
    case "vibrancy":
      return { ...base, ...chrome, vibrancy: "under-window", visualEffectState: "active" };
    case "none":
      return { ...base, ...chrome };
  }
}

/**
 * Pencereyi kurar ve görünüm modu değişince uygular.
 *
 * Saydamlık (Clear) pencere KURULURKEN verilir, sonradan açılıp
 * kapanamaz. Bu yüzden Clear ile diğerleri arasında yeni bir pencere
 * kurulur: konum, boyut ve büyütülmüş hâl taşınır, arayüz Settings
 * ekranında açılır (mod yalnız oradan değiştiriliyor). Acrylic ↔ Mica
 * yerinde değişir.
 *
 * Uygulama İÇİNDE gezinme yok. Bir bağlantıya tıklanırsa kullanıcının
 * tarayıcısında açılır; pencere hiçbir zaman uzak bir içeriğe dönüşemez.
 */

import { BrowserWindow, shell } from "electron";

import type { WindowMode } from "../shared/appearance.ts";
import { needsRecreate, windowOptions, type WindowState } from "./window-options.ts";

export interface WindowHostOptions {
  preloadPath: string;
  rendererIndex: string;
  platform: string;
  showOnReady: boolean;
  iconPath?: string;
}

export interface WindowHost {
  current(): BrowserWindow | null;
  open(mode: WindowMode, view?: string): BrowserWindow;
  applyMode(mode: WindowMode): void;
}

function lockNavigation(win: BrowserWindow): void {
  win.webContents.setWindowOpenHandler(({ url }) => {
    if (url.startsWith("https://")) void shell.openExternal(url);
    return { action: "deny" };
  });
  win.webContents.on("will-navigate", (event) => event.preventDefault());
}

export function createWindowHost(opts: WindowHostOptions): WindowHost {
  let win: BrowserWindow | null = null;
  let mode: WindowMode | null = null;

  function open(next: WindowMode, view?: string, state?: WindowState): BrowserWindow {
    const created = new BrowserWindow({ ...windowOptions(next, opts.platform, opts.preloadPath, state), ...(opts.iconPath ? { icon: opts.iconPath } : {}) });
    lockNavigation(created);
    created.once("ready-to-show", () => {
      if (state?.maximized) created.maximize();
      if (opts.showOnReady) created.show();
    });
    void created.loadFile(opts.rendererIndex, view ? { query: { view } } : undefined);
    created.on("closed", () => {
      if (win === created) {
        win = null;
        mode = null;
      }
    });
    win = created;
    mode = next;
    return created;
  }

  function applyMode(next: WindowMode): void {
    if (!win || !mode || next === mode) return;
    if (!needsRecreate(mode, next)) {
      win.setBackgroundMaterial(next === "mica" ? "mica" : "acrylic");
      mode = next;
      return;
    }
    const old = win;
    open(next, "settings", { bounds: old.getNormalBounds(), maximized: old.isMaximized() });
    old.destroy();
  }

  return { current: () => win, open: (m, view) => open(m, view), applyMode };
}

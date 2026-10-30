/**
 * Pencereyi kurar. Uygulama İÇİNDE gezinme yok. Bir bağlantıya tıklanırsa
 * kullanıcının tarayıcısında açılır; pencere hiçbir zaman uzak bir içeriğe
 * dönüşemez.
 */

import { BrowserWindow, shell } from "electron";

import type { Material } from "../shared/appearance.ts";
import { windowOptions } from "./window-options.ts";

export interface WindowHostOptions {
  preloadPath: string;
  rendererIndex: string;
  platform: string;
  material: Material;
  showOnReady: boolean;
  iconPath?: string;
}

export interface WindowHost {
  current(): BrowserWindow | null;
  open(): BrowserWindow;
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

  function open(): BrowserWindow {
    const created = new BrowserWindow({ ...windowOptions(opts.material, opts.platform, opts.preloadPath), ...(opts.iconPath ? { icon: opts.iconPath } : {}) });
    lockNavigation(created);
    created.once("ready-to-show", () => {
      if (opts.showOnReady) created.show();
    });
    void created.loadFile(opts.rendererIndex);
    created.on("closed", () => {
      if (win === created) win = null;
    });
    win = created;
    return created;
  }

  return { current: () => win, open };
}

/**
 * Pencereyi kurar. Uygulama İÇİNDE gezinme yok. Bir bağlantıya tıklanırsa
 * kullanıcının tarayıcısında açılır; pencere hiçbir zaman uzak bir içeriğe
 * dönüşemez.
 */

import { BrowserWindow, shell } from "electron";

import type { Theme, WindowLook } from "../shared/appearance.ts";
import type { WindowAccent } from "./window-accent.ts";
import { lookChange, windowOptions } from "./window-options.ts";

export interface WindowHostOptions {
  preloadPath: string;
  rendererIndex: string;
  platform: string;
  accent: WindowAccent | null;
  look: WindowLook;
  showOnReady: boolean;
  iconPath?: string;
}

export interface WindowHost {
  current(): BrowserWindow | null;
  open(): BrowserWindow;
  setLook(look: WindowLook): void;
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
  let look = opts.look;

  function applyAccent(target: BrowserWindow, scheme: Theme): void {
    // Windows reddederse cam tamamen gitmesin: Electron'un Acrylic'i odakta yine çalışır.
    if (!opts.accent?.set(target, true, scheme)) target.setBackgroundMaterial("acrylic");
  }

  function open(): BrowserWindow {
    const created = new BrowserWindow({ ...windowOptions(look, opts.platform, opts.preloadPath), ...(opts.iconPath ? { icon: opts.iconPath } : {}) });
    lockNavigation(created);
    if (look.material === "accent") applyAccent(created, look.scheme);
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

  function setLook(next: WindowLook): void {
    const change = lookChange(look, next);
    look = next;
    if (!win) return;
    if (change.background !== null) win.setBackgroundColor(change.background);
    if (change.accent !== null) applyAccent(win, change.accent);
  }

  return { current: () => win, open, setLook };
}

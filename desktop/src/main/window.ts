/**
 * Pencereyi kurar. Uygulama İÇİNDE gezinme yok. Bir bağlantıya tıklanırsa
 * kullanıcının tarayıcısında açılır; pencere hiçbir zaman uzak bir içeriğe
 * dönüşemez.
 */

import { BrowserWindow, shell } from "electron";

import type { Material, WindowLook } from "../shared/appearance.ts";
import type { WindowAccent } from "./window-accent.ts";
import { windowOptions } from "./window-options.ts";

export interface WindowHostOptions {
  preloadPath: string;
  rendererIndex: string;
  platform: string;
  /** Bu sistemin yerel malzemesi; `none` ise malzeme çağrıları yapılmaz. */
  available: Material;
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

  function applyAccent(target: BrowserWindow, on: boolean): void {
    // Windows reddederse cam tamamen gitmesin: Electron'un Acrylic'i odakta yine çalışır.
    if (!opts.accent?.set(target, on) && on) target.setBackgroundMaterial("acrylic");
  }

  function open(): BrowserWindow {
    const created = new BrowserWindow({ ...windowOptions(look, opts.platform, opts.preloadPath), ...(opts.iconPath ? { icon: opts.iconPath } : {}) });
    lockNavigation(created);
    if (look.material === "accent") applyAccent(created, true);
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
    // Yazı tipi ya da dil kaydı da buradan geçer: aynı malzemeyi yeniden kurmak titretir.
    const unchanged = next.material === look.material && next.background === look.background;
    look = next;
    if (!win || unchanged) return;
    win.setBackgroundColor(next.background);
    if (opts.available === "accent") applyAccent(win, next.material === "accent");
    if (opts.available === "acrylic") win.setBackgroundMaterial(next.material === "acrylic" ? "acrylic" : "none");
    if (opts.available === "vibrancy") win.setVibrancy(next.material === "vibrancy" ? "under-window" : null);
  }

  return { current: () => win, open, setLook };
}

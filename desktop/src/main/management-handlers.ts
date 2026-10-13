import { BrowserWindow, dialog, ipcMain } from "electron";
import type { IpcMainInvokeEvent } from "electron";
import type { Language } from "../shared/appearance.ts";
import type { SidecarClient } from "../shared/sidecar-client.ts";
import { createManagementHandlers } from "./management-core.ts";

/** Electron bağlaması: asıl mantık Electron'suz management-core.ts'te. */
export function registerManagementHandlers(client: () => SidecarClient, syntheticFixture: boolean, language: () => Promise<Language>): void {
  createManagementHandlers({
    handle: (channel, fn) => ipcMain.handle(channel, (event, ...args) => fn(event, ...args)),
    call: (method, params, options) => client().call(method, params, options),
    showDialog: async (event, options) => {
      const win = BrowserWindow.fromWebContents((event as IpcMainInvokeEvent).sender);
      if (!win) throw new Error("confirmation window is unavailable");
      return dialog.showMessageBox(win, options);
    },
    language,
    isFixture: syntheticFixture,
  });
}

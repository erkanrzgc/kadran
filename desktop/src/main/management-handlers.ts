import { BrowserWindow, dialog, ipcMain } from "electron";
import type { IpcMainInvokeEvent } from "electron";
import { CHANNELS } from "../shared/channels.ts";
import type { SidecarClient } from "../shared/sidecar-client.ts";
import { requireTarget } from "./ipc-validate.ts";
import { createManagementGate, requireAppId, requireAppPayload, requireConfirmation, requireRef, requireTail } from "./management-ipc.ts";
import { validateManagementResult } from "./management-results.ts";
import type { Language } from "../shared/appearance.ts";
import { managementCancellation, managementConfirmation } from "./management-confirmation.ts";

export function registerManagementHandlers(client: () => SidecarClient, syntheticFixture: boolean, language: () => Promise<Language>): void {
  const gate = createManagementGate();
  const invoke = (method: string, target: unknown, params: Record<string, unknown> = {}, timeoutMs = 35_000) =>
    client().call(method, { target: requireTarget(target), ...params }, { timeoutMs }).then((value) => validateManagementResult(method, value));
  const confirm = async (event: IpcMainInvokeEvent, target: string, appId: string, method: "apps.delete" | "apps.rollback") => {
    // This adapter is enabled only for the unpackaged, explicitly selected local fake fixture.
    if (syntheticFixture && target === "fake@test") return;
    const win = BrowserWindow.fromWebContents(event.sender);
    if (!win) throw new Error("confirmation window is unavailable");
    const selectedLanguage = await language();
    const { response } = await dialog.showMessageBox(win, managementConfirmation(selectedLanguage, method, target, appId));
    if (response !== 1) throw new Error(managementCancellation(selectedLanguage));
  };
  ipcMain.handle(CHANNELS.appsList, (_e, target) => invoke("apps.list", target));
  ipcMain.handle(CHANNELS.appGet, (_e, target, id) => invoke("apps.get", target, { app_id: requireAppId(id) }));
  ipcMain.handle(CHANNELS.appCreate, (_e, rawTarget, rawSpec) => {
    const target = requireTarget(rawTarget), spec = requireAppPayload(rawSpec, "create"), id = requireAppId(spec.app_id);
    return gate(target, id, () => invoke("apps.create", target, { spec }));
  });
  ipcMain.handle(CHANNELS.appUpdate, (_e, rawTarget, rawId, rawUpdate) => {
    const target = requireTarget(rawTarget), app_id = requireAppId(rawId), update = requireAppPayload(rawUpdate, "update");
    return gate(target, app_id, () => invoke("apps.update", target, { app_id, update }));
  });
  ipcMain.handle(CHANNELS.appDeploy, (_e, rawTarget, rawId, rawRef) => {
    const target = requireTarget(rawTarget), app_id = requireAppId(rawId), ref = requireRef(rawRef);
    return gate(target, app_id, () => invoke("apps.deploy", target, { app_id, ref }, 11 * 60_000));
  });
  for (const [channel, method] of [[CHANNELS.appDelete, "apps.delete"], [CHANNELS.appRollback, "apps.rollback"]] as const) {
    ipcMain.handle(channel, (event, rawTarget, rawId, confirmation) => {
      const target = requireTarget(rawTarget), app_id = requireAppId(rawId);
      requireConfirmation(app_id, confirmation);
      return gate(target, app_id, async () => {
        await confirm(event, target, app_id, method);
        return invoke(method, target, { app_id, confirmation }, 2 * 60_000);
      });
    });
  }
  ipcMain.handle(CHANNELS.appLogs, (_e, target, id, tail) => invoke("apps.logs", target, { app_id: requireAppId(id), tail: requireTail(tail) }));
  ipcMain.handle(CHANNELS.alarmsList, (_e, target) => invoke("alarms.list", target));
  ipcMain.handle(CHANNELS.backupsList, (_e, target) => invoke("backups.list", target));
  ipcMain.handle(CHANNELS.backupCreate, (_e, rawTarget) => {
    const target = requireTarget(rawTarget);
    return gate(target, "$backup", () => invoke("backups.create", target, {}, 2 * 60_000));
  });
}

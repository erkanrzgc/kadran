/**
 * Preload'un açtığı köprünün tipleri. Köprü bir beyaz liste: arayüz
 * yalnız bunları çağırabilir (src/preload/index.cjs).
 */

import type { Appearance, Material } from "../shared/appearance.ts";
import type { Cancellable } from "../shared/management.ts";
import type { AuditListPayload, StatusPayload, VerifyPayload, VersionPayload } from "./lib/types.ts";
import type { AppSpec, AppUpdate, AppsListPayload, AppGetPayload, AppWritePayload, AppDeletePayload, RollbackPayload, DeployPayload, LogsPayload, AlarmsPayload, BackupsPayload, BackupCreatePayload } from "./lib/types.ts";

export interface Profile {
  name: string;
  target: string;
  /** Elle seçilen sağlayıcı etiketi; kart gruplarını belirler. */
  provider?: string;
}

export interface AppearanceBundle {
  appearance: Appearance;
  defaults: Appearance;
  material: Material;
}

export type WindowAction = "minimize" | "maximize" | "close";

export interface KadranBridge {
  readonly platform: string;
  version(): Promise<VersionPayload>;
  status(target: string): Promise<StatusPayload>;
  auditList(target: string, afterSeq: number, limit: number): Promise<AuditListPayload>;
  auditVerify(target: string): Promise<VerifyPayload>;
  appsList(target: string): Promise<AppsListPayload>;
  appGet(target: string, appId: string): Promise<AppGetPayload>;
  appCreate(target: string, spec: AppSpec): Promise<Cancellable<AppWritePayload>>;
  appUpdate(target: string, appId: string, update: AppUpdate): Promise<Cancellable<AppWritePayload>>;
  appDelete(target: string, appId: string, confirmation: string): Promise<Cancellable<AppDeletePayload>>;
  appDeploy(target: string, appId: string, ref: string): Promise<Cancellable<DeployPayload>>;
  appRollback(target: string, appId: string, confirmation: string): Promise<Cancellable<RollbackPayload>>;
  appLogs(target: string, appId: string, tail: number): Promise<LogsPayload>;
  alarmsList(target: string): Promise<AlarmsPayload>;
  backupsList(target: string): Promise<BackupsPayload>;
  backupCreate(target: string): Promise<Cancellable<BackupCreatePayload>>;
  listServers(): Promise<Profile[]>;
  addServer(profile: Profile): Promise<Profile[]>;
  updateServer(target: string, change: { name: string; provider: string }): Promise<Profile[]>;
  removeServer(target: string): Promise<Profile[]>;
  serversImportStatus(): Promise<"imported" | "already" | "failed" | null>;
  loadAppearance(): Promise<AppearanceBundle>;
  saveAppearance(appearance: Appearance): Promise<Appearance>;
  windowControl(action: WindowAction): Promise<void>;
  copyText(text: string): Promise<void>;
}

declare global {
  interface Window {
    readonly kadran: KadranBridge;
  }
}

export const bridge: KadranBridge = window.kadran;

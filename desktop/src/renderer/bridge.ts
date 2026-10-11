/**
 * Preload'un açtığı köprünün tipleri. Köprü bir beyaz liste: arayüz
 * yalnız bunları çağırabilir (src/preload/index.cjs).
 */

import type { Appearance, WindowMode } from "../shared/appearance.ts";
import type { AuditListPayload, StatusPayload, VerifyPayload, VersionPayload } from "./lib/types.ts";
import type { AppSpec, AppUpdate, AppsListPayload, AppGetPayload, AppWritePayload, AppDeletePayload, RollbackPayload, DeployPayload, LogsPayload, AlarmsPayload, BackupsPayload, BackupCreatePayload } from "./lib/types.ts";

export interface Profile {
  name: string;
  target: string;
}

export interface AppearanceBundle {
  appearance: Appearance;
  defaults: Appearance;
  modes: WindowMode[];
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
  appCreate(target: string, spec: AppSpec): Promise<AppWritePayload>;
  appUpdate(target: string, appId: string, update: AppUpdate): Promise<AppWritePayload>;
  appDelete(target: string, appId: string, confirmation: string): Promise<AppDeletePayload>;
  appDeploy(target: string, appId: string, ref: string): Promise<DeployPayload>;
  appRollback(target: string, appId: string, confirmation: string): Promise<RollbackPayload>;
  appLogs(target: string, appId: string, tail: number): Promise<LogsPayload>;
  alarmsList(target: string): Promise<AlarmsPayload>;
  backupsList(target: string): Promise<BackupsPayload>;
  backupCreate(target: string): Promise<BackupCreatePayload>;
  loadProfiles(): Promise<Profile[]>;
  saveProfiles(profiles: Profile[]): Promise<Profile[]>;
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

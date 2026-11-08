/**
 * Arayüzün masaüstüyle konuştuğu tek yer. Yöntemler Go'da
 * (desktop/internal/bridge, Wails ile bağlanıyor; K-165) ve bir beyaz liste:
 * arayüz yalnız bunları çağırabilir. Go tarafı her girdiyi doğrular,
 * durum değiştiren işlemde yerel onay ister ve hatayı arıtılmış döndürür.
 *
 * Buradaki tek iş: Go yöntemlerini arayüzün beklediği biçime bağlamak ve
 * yönetim yanıtlarının biçimini denetlemek (management-results).
 */

import type { Appearance, Material } from "../shared/appearance.ts";
import { isCancelled, type Cancellable } from "../shared/management.ts";
import { validateManagementResult } from "./lib/management-results.ts";
import { parseResources, type ResourceSnapshot } from "./lib/resources.ts";
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
  /** Canlı kaynak kullanımı (K-166); biçim denetlenmiş anlık görüntü. */
  resources(target: string, includeApps: boolean): Promise<ResourceSnapshot>;
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
  /** Eski listenin göç sonucu; Wails sürümünde göç yok (K-165), hep null. */
  serversImportStatus(): Promise<{ result: "imported" | "already" | "failed"; skipped: string[] } | null>;
  loadAppearance(): Promise<AppearanceBundle>;
  saveAppearance(appearance: Appearance): Promise<Appearance>;
  windowControl(action: WindowAction): Promise<void>;
  copyText(text: string): Promise<void>;
}

/** Go'daki yöntemler (desktop/internal/bridge); Wails `window.go` altına koyar. */
interface GoBridge {
  Version(): Promise<unknown>;
  Status(target: string): Promise<unknown>;
  ResourceUsage(target: string, includeApps: boolean): Promise<unknown>;
  AuditList(target: string, afterSeq: number, limit: number): Promise<unknown>;
  AuditVerify(target: string): Promise<unknown>;
  AppsList(target: string): Promise<unknown>;
  AppGet(target: string, appId: string): Promise<unknown>;
  AppCreate(target: string, spec: AppSpec): Promise<unknown>;
  AppUpdate(target: string, appId: string, update: AppUpdate): Promise<unknown>;
  AppDelete(target: string, appId: string, confirmation: string): Promise<unknown>;
  AppDeploy(target: string, appId: string, ref: string): Promise<unknown>;
  AppRollback(target: string, appId: string, confirmation: string): Promise<unknown>;
  AppLogs(target: string, appId: string, tail: number): Promise<unknown>;
  AlarmsList(target: string): Promise<unknown>;
  BackupsList(target: string): Promise<unknown>;
  BackupCreate(target: string): Promise<unknown>;
  ListServers(): Promise<Profile[]>;
  AddServer(profile: Profile): Promise<Profile[]>;
  UpdateServer(target: string, change: { name: string; provider: string }): Promise<Profile[]>;
  RemoveServer(target: string): Promise<Profile[]>;
  LoadAppearance(): Promise<AppearanceBundle>;
  SaveAppearance(appearance: Appearance): Promise<Appearance>;
  WindowControl(action: WindowAction): Promise<void>;
  CopyText(text: string): Promise<void>;
}

declare global {
  interface Window {
    readonly go?: { readonly bridge?: { readonly Bridge?: GoBridge } };
  }
}

/** Bağlar sayfa yüklenince kurulur; çağrı anında aranır. */
function goBridge(): GoBridge {
  const found = window.go?.bridge?.Bridge;
  if (!found) throw new Error("the desktop bridge is not available");
  return found;
}

/** Çağrı: bağ yoksa eşzamanlı hata da reddedilmiş söz olur. */
function call<T>(run: (go: GoBridge) => Promise<unknown>): Promise<T> {
  return Promise.resolve().then(() => run(goBridge())) as Promise<T>;
}

/**
 * Yönetim yanıtı: biçimi denetlenir. İptal yalnız Go'daki yerel onaydan
 * gelir ve olduğu gibi geçer.
 */
function managed<T>(method: string, run: (go: GoBridge) => Promise<unknown>): Promise<T> {
  return call<unknown>(run).then((value) => (isCancelled(value) ? value : validateManagementResult(method, value))) as Promise<T>;
}

/** Başlık çubuğu macOS'ta trafik ışıklarına yer bırakır. */
function platformOf(userAgent: string): string {
  if (/Macintosh|Mac OS X/.test(userAgent)) return "darwin";
  if (/Windows/.test(userAgent)) return "win32";
  return "linux";
}

export const bridge: KadranBridge = {
  platform: platformOf(navigator.userAgent),
  version: () => call((go) => go.Version()),
  status: (target) => call((go) => go.Status(target)),
  resources: (target, includeApps) => call<unknown>((go) => go.ResourceUsage(target, includeApps)).then(parseResources),
  auditList: (target, afterSeq = 0, limit = 50) => call((go) => go.AuditList(target, afterSeq, limit)),
  auditVerify: (target) => call((go) => go.AuditVerify(target)),
  appsList: (target) => managed("apps.list", (go) => go.AppsList(target)),
  appGet: (target, appId) => managed("apps.get", (go) => go.AppGet(target, appId)),
  appCreate: (target, spec) => managed("apps.create", (go) => go.AppCreate(target, spec)),
  appUpdate: (target, appId, update) => managed("apps.update", (go) => go.AppUpdate(target, appId, update)),
  appDelete: (target, appId, confirmation) => managed("apps.delete", (go) => go.AppDelete(target, appId, confirmation)),
  appDeploy: (target, appId, ref) => managed("apps.deploy", (go) => go.AppDeploy(target, appId, ref)),
  appRollback: (target, appId, confirmation) => managed("apps.rollback", (go) => go.AppRollback(target, appId, confirmation)),
  appLogs: (target, appId, tail) => managed("apps.logs", (go) => go.AppLogs(target, appId, tail)),
  alarmsList: (target) => managed("alarms.list", (go) => go.AlarmsList(target)),
  backupsList: (target) => managed("backups.list", (go) => go.BackupsList(target)),
  backupCreate: (target) => managed("backups.create", (go) => go.BackupCreate(target)),
  listServers: () => call((go) => go.ListServers()),
  addServer: (profile) => call((go) => go.AddServer(profile)),
  updateServer: (target, change) => call((go) => go.UpdateServer(target, change)),
  removeServer: (target) => call((go) => go.RemoveServer(target)),
  serversImportStatus: () => Promise.resolve(null),
  loadAppearance: () => call((go) => go.LoadAppearance()),
  saveAppearance: (appearance) => call((go) => go.SaveAppearance(appearance)),
  windowControl: (action) => call((go) => go.WindowControl(action)),
  copyText: (text) => call((go) => go.CopyText(text)),
};

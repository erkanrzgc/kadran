/**
 * Preload'un açtığı köprünün tipleri. Köprü bir beyaz liste: arayüz
 * yalnız bunları çağırabilir (src/preload/index.cjs).
 */

import type { Appearance, WindowMode } from "../shared/appearance.ts";
import type { AuditListPayload, StatusPayload, VerifyPayload, VersionPayload } from "./lib/types.ts";

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

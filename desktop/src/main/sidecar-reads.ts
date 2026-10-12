/**
 * Durum, sürüm ve denetim okuma kanalları (Electron'suz çekirdek; bağlama
 * index.ts'te). Hatalar renderer'a ham değil, sidecar-error.ts'ten arıtılmış
 * gider.
 */

import { CHANNELS } from "../shared/channels.ts";
import { requireTarget, toUint } from "./ipc-validate.ts";
import { callForUi, type SidecarCall } from "./sidecar-error.ts";

export interface ReadDeps {
  handle(channel: string, fn: (event: unknown, ...args: unknown[]) => unknown): void;
  call: SidecarCall;
}

export function registerSidecarReads(deps: ReadDeps): void {
  const call = (method: string, params?: Record<string, unknown>) => callForUi(deps.call, method, params);
  deps.handle(CHANNELS.version, () => call("version"));
  deps.handle(CHANNELS.status, (_e, target) => call("status", { target: requireTarget(target) }));
  deps.handle(CHANNELS.auditList, (_e, target, afterSeq, limit) =>
    call("audit.list", { target: requireTarget(target), after_seq: toUint(afterSeq, 0), limit: toUint(limit, 50) }),
  );
  deps.handle(CHANNELS.auditVerify, (_e, target) => call("audit.verify", { target: requireTarget(target) }));
}

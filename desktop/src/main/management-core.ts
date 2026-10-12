/**
 * Yönetim kanallarının Electron'suz çekirdeği. `electron` buraya GİRMEZ:
 * mutasyon CI işi Electron ikilisi olmadan kuruluyor ve gerçek bir import
 * onun index.js'ini çalıştırıp ikili indirmeye kalkar. Electron bağlaması
 * management-handlers.ts'te.
 *
 * Durum değiştiren HER kanalda sıra: girdiyi doğrula, kapıya gir, ana
 * süreçte yerel onay göster, onaylanırsa sidecar'ı çağır. Zaman aşımı
 * onaydan SONRA başlar. İptal bir hata değil, tipli sonuçtur.
 */

import type { Language } from "../shared/appearance.ts";
import { CHANNELS } from "../shared/channels.ts";
import { cancelled } from "../shared/management.ts";
import { requireTarget } from "./ipc-validate.ts";
import { managementConfirmation, type ConfirmationOptions, type ConfirmRequest } from "./management-confirmation.ts";
import { createManagementGate, requireAppId, requireAppPayload, requireConfirmation, requireRef, requireTail } from "./management-ipc.ts";
import { validateManagementResult } from "./management-results.ts";

export interface ManagementDeps {
  handle(channel: string, fn: (event: unknown, ...args: unknown[]) => unknown): void;
  call(method: string, params: Record<string, unknown>, options: { timeoutMs: number }): Promise<unknown>;
  showDialog(event: unknown, options: ConfirmationOptions): Promise<{ response: number }>;
  language(): Promise<Language>;
  /** Yalnız paketlenmemiş, açıkça seçilmiş yerel sahte sidecar düzeneğinde true. */
  isFixture: boolean;
}

/** Onay düğmesinin sırası: 0 vazgeç, 1 onay. */
const CONFIRM_BUTTON = 1;
const READ_TIMEOUT_MS = 35_000;

/**
 * Durum değiştiren çağrıların IPC süresi, sidecar'ın aynı yöntem için
 * kullandığı süreden (cmd/kadran/sidecar_management.go) bir dakika UZUN.
 * Önce sidecar düşmeli ve gerçek sebebi döndürmeli: masaüstü önce
 * vazgeçerse kapı serbest kalır ve iş sunucuda sürerken aynı geri alma
 * ikinci kez başlatılabilir (uygulama bir sürüm DAHA geri gider). Test,
 * değerleri Go kaynağından okuyup bu kuralı denetler.
 */
const SIDECAR_MARGIN_MS = 60_000;
const DEPLOY_TIMEOUT_MS = 10 * 60_000 + SIDECAR_MARGIN_MS;
const LIFECYCLE_TIMEOUT_MS = 4 * 60_000 + SIDECAR_MARGIN_MS;
const BACKUP_TIMEOUT_MS = 90_000 + SIDECAR_MARGIN_MS;

export function createManagementHandlers(deps: ManagementDeps): void {
  const gate = createManagementGate();
  const invoke = (method: string, target: unknown, params: Record<string, unknown> = {}, timeoutMs = READ_TIMEOUT_MS) =>
    deps.call(method, { target: requireTarget(target), ...params }, { timeoutMs }).then((value) => validateManagementResult(method, value));
  const confirm = async (event: unknown, request: ConfirmRequest): Promise<boolean> => {
    // Yalnızca paketlenmemiş yerel sahte düzenekte ve yalnız fake@test hedefinde atlanır.
    if (deps.isFixture && request.target === "fake@test") return true;
    const options = managementConfirmation(await deps.language(), request);
    const { response } = await deps.showDialog(event, options);
    return response === CONFIRM_BUTTON;
  };

  deps.handle(CHANNELS.appsList, (_e, target) => invoke("apps.list", target));
  deps.handle(CHANNELS.appGet, (_e, target, id) => invoke("apps.get", target, { app_id: requireAppId(id) }));
  deps.handle(CHANNELS.appCreate, (event, rawTarget, rawSpec) => {
    const target = requireTarget(rawTarget), spec = requireAppPayload(rawSpec, "create"), id = requireAppId(spec.app_id);
    return gate(target, id, async () => {
      if (!(await confirm(event, { kind: "create", target, spec }))) return cancelled();
      return invoke("apps.create", target, { spec });
    });
  });
  deps.handle(CHANNELS.appUpdate, (event, rawTarget, rawId, rawUpdate) => {
    const target = requireTarget(rawTarget), app_id = requireAppId(rawId), update = requireAppPayload(rawUpdate, "update");
    return gate(target, app_id, async () => {
      if (!(await confirm(event, { kind: "update", target, app: app_id, update }))) return cancelled();
      return invoke("apps.update", target, { app_id, update });
    });
  });
  deps.handle(CHANNELS.appDeploy, (event, rawTarget, rawId, rawRef) => {
    const target = requireTarget(rawTarget), app_id = requireAppId(rawId), ref = requireRef(rawRef);
    return gate(target, app_id, async () => {
      if (!(await confirm(event, { kind: "deploy", target, app: app_id, ref }))) return cancelled();
      return invoke("apps.deploy", target, { app_id, ref }, DEPLOY_TIMEOUT_MS);
    });
  });
  deps.handle(CHANNELS.appDelete, (event, rawTarget, rawId, confirmation) => {
    const target = requireTarget(rawTarget), app_id = requireAppId(rawId);
    requireConfirmation(app_id, confirmation);
    return gate(target, app_id, async () => {
      if (!(await confirm(event, { kind: "delete", target, app: app_id }))) return cancelled();
      return invoke("apps.delete", target, { app_id, confirmation }, LIFECYCLE_TIMEOUT_MS);
    });
  });
  deps.handle(CHANNELS.appRollback, (event, rawTarget, rawId, confirmation) => {
    const target = requireTarget(rawTarget), app_id = requireAppId(rawId);
    requireConfirmation(app_id, confirmation);
    return gate(target, app_id, async () => {
      if (!(await confirm(event, { kind: "rollback", target, app: app_id }))) return cancelled();
      return invoke("apps.rollback", target, { app_id, confirmation }, LIFECYCLE_TIMEOUT_MS);
    });
  });
  deps.handle(CHANNELS.appLogs, (_e, target, id, tail) => invoke("apps.logs", target, { app_id: requireAppId(id), tail: requireTail(tail) }));
  deps.handle(CHANNELS.alarmsList, (_e, target) => invoke("alarms.list", target));
  deps.handle(CHANNELS.backupsList, (_e, target) => invoke("backups.list", target));
  deps.handle(CHANNELS.backupCreate, (event, rawTarget) => {
    const target = requireTarget(rawTarget);
    return gate(target, "$backup", async () => {
      if (!(await confirm(event, { kind: "backup", target }))) return cancelled();
      return invoke("backups.create", target, {}, BACKUP_TIMEOUT_MS);
    });
  });
}

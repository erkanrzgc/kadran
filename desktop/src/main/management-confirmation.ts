/**
 * Yerel onay kutusunun içeriği. Metin ana süreçte, DOĞRULANMIŞ değerlerden
 * kurulur: renderer'ın ne yazdığı değil, sidecar'a gidecek olan gösterilir.
 * Ortam değişkeni DEĞERLERİ hiçbir koşulda kutuya girmez; yalnız anahtarlar.
 * Metinler src/shared/dialog-text.ts kataloğundan gelir (en/tr).
 */

import type { Language } from "../shared/appearance.ts";
import { dialogText, type DialogKey } from "../shared/dialog-text.ts";

type Obj = Record<string, unknown>;

export type ConfirmRequest =
  | { kind: "create"; target: string; spec: Obj }
  | { kind: "update"; target: string; app: string; update: Obj }
  | { kind: "deploy"; target: string; app: string; ref: string }
  | { kind: "delete"; target: string; app: string }
  | { kind: "rollback"; target: string; app: string }
  | { kind: "backup"; target: string };

export interface ConfirmationOptions {
  type: "warning";
  title: string;
  message: string;
  detail: string;
  buttons: [string, string];
  defaultId: 0;
  cancelId: 0;
  noLink: true;
}

const MAX_VALUE_CHARS = 200;
const MAX_LIST_ITEMS = 10;

/** Denetim ve biçim karakterlerini (bidi dahil) görünür kaçışa çevirir; uzunu keser. */
function safe(value: unknown): string {
  const text = typeof value === "string" ? value : "";
  const shown = text.length > MAX_VALUE_CHARS ? text.slice(0, MAX_VALUE_CHARS) + "…" : text;
  return shown.replace(/[\p{Cc}\p{Cf}]/gu, (ch) => "\\u" + ch.codePointAt(0)!.toString(16).padStart(4, "0"));
}

const record = (v: unknown): Obj => (typeof v === "object" && v !== null && !Array.isArray(v) ? (v as Obj) : {});
const list = (v: unknown): unknown[] => (Array.isArray(v) ? v : []);

export function managementConfirmation(language: Language, request: ConfirmRequest): ConfirmationOptions {
  const text = (key: DialogKey, params: Record<string, string | number> = {}) => dialogText(language, key, params);
  const names = (items: string[]) => {
    const shown = items.slice(0, MAX_LIST_ITEMS).map(safe).join(", ");
    return items.length > MAX_LIST_ITEMS ? `${shown}, ${text("line.more", { count: items.length - MAX_LIST_ITEMS })}` : shown;
  };
  const lines: string[] = [text("line.server", { target: safe(request.target) })];
  const app = request.kind === "create" ? safe(request.spec.app_id) : request.kind === "backup" ? "" : safe(request.app);

  switch (request.kind) {
    case "deploy":
      lines.push(request.ref === "" ? text("line.ref_default") : text("line.ref", { ref: safe(request.ref) }), text("line.deploy_effect"));
      break;
    case "rollback":
      lines.push(text("line.rollback_effect"));
      break;
    case "delete":
      lines.push(text("line.delete_effect"));
      break;
    case "backup":
      lines.push(text("line.backup_effect"));
      break;
    case "create": {
      const s = request.spec;
      lines.push(text("line.source", { host: safe(s.git_host), owner: safe(s.git_owner), repo: safe(s.git_repo) }), text("line.branch", { branch: safe(s.git_branch) }));
      lines.push(s.domain ? text("line.domain", { domain: safe(s.domain) }) : text("line.no_domain"), text("line.port", { port: String(s.container_port) }));
      if (s.dockerfile_path) lines.push(text("line.dockerfile", { path: safe(s.dockerfile_path) }));
      lines.push(...volumeLines(text, list(s.volumes)));
      const keys = Object.keys(record(s.env));
      if (keys.length > 0) lines.push(text("line.env_set", { keys: names(keys) }));
      break;
    }
    case "update": {
      const u = request.update;
      if (u.domain !== undefined) lines.push(u.domain ? text("line.domain", { domain: safe(u.domain) }) : text("line.no_domain"));
      if (u.git_branch !== undefined) lines.push(text("line.branch", { branch: safe(u.git_branch) }));
      if (u.health_path !== undefined) lines.push(u.health_path ? text("line.health", { path: safe(u.health_path) }) : text("line.health_none"));
      if (u.replicas !== undefined) lines.push(text("line.replicas", { count: String(u.replicas) }));
      const setKeys = Object.keys(record(u.env));
      if (setKeys.length > 0) lines.push(text("line.env_set", { keys: names(setKeys) }));
      const removedEnv = list(u.env_remove).map(String);
      if (removedEnv.length > 0) lines.push(text("line.env_remove", { keys: names(removedEnv) }));
      lines.push(...volumeLines(text, list(u.volumes)));
      const detached = list(u.volume_remove).map(String);
      if (detached.length > 0) lines.push(text("line.volume_detach", { names: names(detached) }));
      break;
    }
  }

  const action = text(`action.${request.kind}`);
  return {
    type: "warning",
    title: text("title"),
    message: text(`message.${request.kind}`, { app }),
    detail: lines.join("\n"),
    buttons: [text("cancel"), action],
    defaultId: 0,
    cancelId: 0,
    noLink: true,
  };
}

function volumeLines(text: (key: DialogKey, params?: Record<string, string | number>) => string, volumes: unknown[]): string[] {
  const shown = volumes.slice(0, MAX_LIST_ITEMS).map((item) => {
    const v = record(item);
    return text("line.volume", { name: safe(v.name), path: safe(v.mount_path), mode: text(v.read_only === true ? "mode.ro" : "mode.rw") });
  });
  if (volumes.length > MAX_LIST_ITEMS) shown.push(text("line.more", { count: volumes.length - MAX_LIST_ITEMS }));
  return shown;
}

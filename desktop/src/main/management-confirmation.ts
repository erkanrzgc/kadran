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

/** Denetim, biçim (bidi dahil), satır/paragraf ayırıcı ve düz boşluk dışındaki boşluklar. */
const INVISIBLE = /^(?:[\p{Cc}\p{Cf}\p{Zl}\p{Zp}]|(?! )\p{Zs})$/u;

/**
 * Görünmez karakterleri görünür kaçışa çevirir, SONRA uzunu ORTADAN keser.
 * Önce kesilseydi 200 karakter kaçışla ~1200'e büyürdü; baştan kesilseydi
 * `user@host` hedefinin host'u (kuyruk) kutuda görünmezdi.
 */
function safe(value: unknown): string {
  const text = typeof value === "string" ? value : "";
  const parts = Array.from(text, (ch) => (INVISIBLE.test(ch) ? "\\u" + ch.codePointAt(0)!.toString(16).padStart(4, "0") : ch));
  const whole = parts.join("");
  if (whole.length <= MAX_VALUE_CHARS) return whole;
  const half = Math.floor(MAX_VALUE_CHARS / 2);
  let head = "";
  let i = 0;
  while (i < parts.length && head.length + parts[i]!.length <= half) head += parts[i++];
  let tail = "";
  for (let j = parts.length - 1; j >= i && tail.length + parts[j]!.length <= half; j--) tail = parts[j] + tail;
  return `${head}…${tail}`;
}

const MIB = 1024n * 1024n;

/** Bayt sayısı (u64 metni) → MiB'e tam bölünüyorsa "8192 MiB", değilse yuvarlamadan bayt. */
function memory(value: unknown): string {
  const text = typeof value === "string" ? value : "";
  if (!/^\d{1,20}$/.test(text)) return safe(text);
  const bytes = BigInt(text);
  return bytes > 0n && bytes % MIB === 0n ? `${bytes / MIB} MiB` : `${bytes} B`;
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
      // Kopya sayısı ve sınırlar kaynak tüketimini belirler: 64 kopya ya da
      // dev bir bellek sınırı kutuda görünmeden onaylanmasın.
      lines.push(text("line.replicas", { count: safe(String(s.replicas ?? "")) }));
      lines.push(s.health_path ? text("line.health", { path: safe(s.health_path) }) : text("line.health_none"));
      const limits = record(s.limits);
      lines.push(text("line.limits", { memory: memory(limits.memory_bytes), cpu: safe(String(limits.cpu_millis ?? "")), io: safe(String(limits.blkio_weight ?? "")) }));
      if (s.dockerfile_path) lines.push(text("line.dockerfile", { path: safe(s.dockerfile_path) }));
      lines.push(...volumeLines(text, list(s.volumes)));
      const keys = Object.keys(record(s.env));
      if (keys.length > 0) lines.push(text("line.env_set", { keys: names(keys) }));
      const buildArgs = Object.keys(record(s.build_args));
      if (buildArgs.length > 0) lines.push(text("line.build_args", { keys: names(buildArgs) }));
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

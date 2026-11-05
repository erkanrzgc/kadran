/**
 * Ortak sunucu listesi (servers.json, K-162): ana sürecin kanalları ve eski
 * profil listesinin bir kez taşınması.
 *
 * Dosyayı bu süreç okuyup yazmaz; biçimi yalnız Go tarafı (internal/servers)
 * bilir ve yan süreç `servers.*` yöntemleriyle uygular. Her değişiklik tek
 * kayıt gönderir, bütün listeyi değil: CLI'ın o arada eklediği sunucu silinmez.
 */

import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";

import { CHANNELS } from "../shared/channels.ts";
import { SidecarError } from "../shared/sidecar-client.ts";
import { requireTarget } from "./ipc-validate.ts";
import { profilesPath, type Profile } from "./profiles.ts";
import { userFacingError, type SidecarCall } from "./sidecar-error.ts";
import type { ReadDeps } from "./sidecar-reads.ts";

/** Göçün bittiğini söyleyen dosya (Electron kullanıcı verisi dizininde). */
export const IMPORT_MARKER = "servers-imported";

export type ImportResult = "imported" | "already" | "failed";

/** Göçün sonucu ve alınamayan hedefler: göç bir kez koşar, kaybolan kart söylenmeli. */
export interface ImportStatus {
  result: ImportResult;
  skipped: string[];
}

/** Eski masaüstü en çok 100 profil gösteriyordu; yan sürecin içe aktarma sınırı da bu. */
const MAX_IMPORT = 100;

/** Ad sınırı Go'da 64; burada yalnız kaba bir üst sınır, kırpma ve kural Go'da. */
const MAX_NAME = 200;
const MAX_PROVIDER = 64;

/** Yan sürecin çevrilebilir sebepleri (cmd/kadran/sidecar_servers.go). */
const REASONS = new Set(["duplicate_name", "duplicate_target", "not_found", "newer_file"]);

function requireName(value: unknown): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error("server name is empty");
  if (value.length > MAX_NAME) throw new Error("server name is too long");
  return value;
}

function requireProvider(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value !== "string") throw new Error("provider must be text");
  if ([...value].length > MAX_PROVIDER) throw new Error("provider is too long");
  return value;
}

function requireObject(value: unknown): Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("invalid server");
  return value as Record<string, unknown>;
}

/** Yan sürecin listesinden yalnız arayüzün alanları: yönetici hedefi ve sudo dosyada kalır. */
function toProfiles(result: unknown): Profile[] {
  const servers = (result as { servers?: unknown } | null)?.servers;
  if (!Array.isArray(servers)) throw new Error("invalid server list from the sidecar");
  return servers.map((raw) => {
    const s = requireObject(raw);
    if (typeof s.name !== "string" || typeof s.target !== "string") throw new Error("invalid server list from the sidecar");
    return { name: s.name, target: s.target, ...(typeof s.provider === "string" && s.provider !== "" ? { provider: s.provider } : {}) };
  });
}

/**
 * Bilinen sebep renderer'a sabit bir kodla gider ("server-list:duplicate_name"),
 * arayüz onu kendi dilinde söyler; geri kalan hata arıtılmış metniyle.
 */
function serverListError(err: unknown): Error {
  if (err instanceof SidecarError && err.data && REASONS.has(err.data)) return new Error(`server-list:${err.data}`);
  return userFacingError(err);
}

export interface ServerListDeps {
  handle: ReadDeps["handle"];
  call: SidecarCall;
  /** Açılıştaki göçün sonucu; henüz koşmadıysa null. */
  importStatus: () => ImportStatus | null;
}

export function registerServerList(deps: ServerListDeps): void {
  const run = async (method: string, params?: Record<string, unknown>): Promise<Profile[]> => {
    try {
      return toProfiles(await deps.call(method, params));
    } catch (err) {
      throw serverListError(err);
    }
  };
  deps.handle(CHANNELS.serversList, () => run("servers.list"));
  deps.handle(CHANNELS.serversAdd, (_e, profile) => {
    const p = requireObject(profile);
    return run("servers.add", { name: requireName(p.name), target: requireTarget(p.target), provider: requireProvider(p.provider) });
  });
  deps.handle(CHANNELS.serversUpdate, (_e, target, change) => {
    const c = requireObject(change);
    return run("servers.update", { target: requireTarget(target), name: requireName(c.name), provider: requireProvider(c.provider) });
  });
  deps.handle(CHANNELS.serversRemove, (_e, target) => run("servers.remove", { target: requireTarget(target) }));
  deps.handle(CHANNELS.serversImportStatus, () => deps.importStatus());
}

/**
 * importProfiles, masaüstünün eski listesini (profiles.json) bir kez ortak
 * listeye taşır. Ad düzeltme ve tekrar atlama Go tarafında (servers.Import).
 *
 * - İşaret dosyası varsa hiçbir şey yapılmaz.
 * - profiles.json yoksa taşınacak bir şey yok: işaret yazılır.
 * - profiles.json bozuksa "taşınacak bir şey yok" DEĞİLDİR: işaret yazılmaz,
 *   sonraki açılış yeniden dener (loadProfiles ikisini de boş liste yapıyor,
 *   bu yüzden dosya burada ayrıca okunur).
 * - profiles.json silinmez ve değişmez: v0.10.0'a dönen listesini görür.
 */
export async function importProfiles(userDataDir: string, call: SidecarCall, warn: (message: string) => void): Promise<ImportStatus> {
  const failed: ImportStatus = { result: "failed", skipped: [] };
  const marker = join(userDataDir, IMPORT_MARKER);
  if (await readFile(marker).then(() => true, () => false)) return { result: "already", skipped: [] };

  let raw: string | null;
  try {
    raw = await readFile(profilesPath(userDataDir), "utf8");
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
      warn(`could not read the old server list: ${(err as Error).message}`);
      return failed;
    }
    raw = null;
  }

  const skipped: string[] = [];
  if (raw !== null) {
    let parsed: unknown;
    try {
      parsed = JSON.parse(raw);
    } catch (err) {
      warn(`the old server list is damaged: ${(err as Error).message}`);
      return failed;
    }
    if (!Array.isArray(parsed)) {
      warn("the old server list is not a list");
      return failed;
    }
    // Yalnız bilinen alanlar: yan süreç bilinmeyen alanı reddeder ve elle
    // eklenmiş bir alan göçü sonsuza dek düşürürdü.
    const all: Profile[] = parsed
      .filter((p): p is Profile => typeof p === "object" && p !== null && typeof p.name === "string" && typeof p.target === "string")
      .map((p) => ({ name: p.name, target: p.target, ...(typeof p.provider === "string" ? { provider: p.provider } : {}) }));
    // Fazlası yan süreçte reddedilir ve göç hiç bitmezdi: ilk 100 gider, gerisi söylenir.
    const profiles = all.slice(0, MAX_IMPORT);
    if (profiles.length > 0) {
      try {
        const result = (await call("servers.import", { profiles })) as { skipped?: { target?: unknown }[] } | null;
        for (const s of result?.skipped ?? []) if (typeof s?.target === "string") skipped.push(s.target);
      } catch (err) {
        warn(`could not import the old server list: ${userFacingError(err).message}`);
        return failed;
      }
    }
    skipped.push(...all.slice(MAX_IMPORT).map((p) => p.target));
    if (skipped.length > 0) warn(`not imported from the old server list: ${skipped.join(", ")}`);
  }

  await writeFile(marker, `${new Date().toISOString()}\n`);
  return { result: "imported", skipped };
}

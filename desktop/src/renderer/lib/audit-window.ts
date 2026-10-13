/**
 * Audit sayfalaması.
 *
 * Sunucu `after_seq`'i HARİÇ tutar ve kayıtları ARTAN sırada döner
 * (internal/store/store.go: WHERE seq > ? ORDER BY seq ASC LIMIT ?).
 * "En yeni N" için önce latest_seq okunur, sonra after_seq = latest − N
 * istenir. Eski arayüz (0, 50) istiyordu ve en ESKİ 50'yi gösteriyordu.
 */

import type { AuditRecord } from "./types.ts";

export const AUDIT_PAGE = 50;
const MAX_LIMIT = 1000;

export interface AuditWindow {
  afterSeq: number;
  limit: number;
}

/**
 * parseLatestSeq, sunucunun latest_seq alanını okur. protojson uint64'ü
 * dize yazar ve boş zinciri tam "0" olarak gönderir (EmitUnpopulated).
 * Başka her biçim null döner: bozuk cevap "zincirde kayıt yok" diye
 * gösterilmemeli, hata olarak gösterilmeli.
 */
export function parseLatestSeq(raw: unknown): number | null {
  if (typeof raw !== "string" || !/^(0|[1-9][0-9]*)$/.test(raw)) return null;
  const n = Number(raw);
  return Number.isSafeInteger(n) ? n : null;
}

export function clampLimit(n: number): number {
  if (!Number.isFinite(n) || n < 1) return 1;
  return Math.min(MAX_LIMIT, Math.floor(n));
}

export function newestWindow(latestSeq: number, n: number): AuditWindow {
  const limit = clampLimit(n);
  return { afterSeq: Math.max(0, latestSeq - limit), limit };
}

/**
 * newestPageIntact: latest_seq zincirin başı (store.AuditHead). Artan
 * sırada ve limitli olduğu için en yeni pencere, sonradan yeni kayıt
 * eklense bile, o kaydı her zaman içerir. İçermiyorsa cevap eksiktir;
 * "zincirde kayıt yok" diye gösterilmemeli.
 */
export function newestPageIntact(latestSeq: number, records: readonly AuditRecord[]): boolean {
  const head = String(latestSeq);
  return records.some((r) => r.seq === head);
}

/** olderWindow, ekrandaki en eski kaydın hemen öncesindeki n kaydı ister. */
export function olderWindow(oldestShown: number, n: number): AuditWindow | null {
  if (!Number.isFinite(oldestShown) || oldestShown <= 1) return null;
  const limit = Math.min(clampLimit(n), oldestShown - 1);
  return { afterSeq: oldestShown - 1 - limit, limit };
}

/** mergeNewestFirst, iki listeyi birleştirir; aynı seq bir kez, en yeni üstte. */
export function mergeNewestFirst(current: readonly AuditRecord[], incoming: readonly AuditRecord[]): AuditRecord[] {
  const bySeq = new Map<string, AuditRecord>();
  for (const r of [...current, ...incoming]) bySeq.set(r.seq, r);
  return [...bySeq.values()].sort((a, b) => Number(b.seq) - Number(a.seq));
}

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

export function clampLimit(n: number): number {
  if (!Number.isFinite(n) || n < 1) return 1;
  return Math.min(MAX_LIMIT, Math.floor(n));
}

export function newestWindow(latestSeq: number, n: number): AuditWindow {
  const limit = clampLimit(n);
  return { afterSeq: Math.max(0, latestSeq - limit), limit };
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

/**
 * Denetim kayıtları, en yeni üstte. Önce latest_seq okunur (limit 1),
 * sonra after_seq = latest − n istenir (lib/audit-window.ts). Eski arayüz
 * (0, 50) istiyor ve EN ESKİ 50'yi "son kayıtlar" diye gösteriyordu.
 *
 * Bayat cevap disiplini: her await'ten sonraki her durum yazımı ve her
 * bildirim, hedefin ve "nesil"in hâlâ aynı olduğuna bakar. Nesil, hedef
 * değişince ya da en yeni sayfa yeniden yüklenince artar; böylece geç
 * gelen "eskileri yükle" sayfası yeni listenin üstüne binmez.
 */

import { useCallback, useEffect, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { AUDIT_PAGE, mergeNewestFirst, newestWindow, olderWindow } from "../lib/audit-window.ts";
import { errorText } from "../lib/errors.ts";
import type { AuditRecord } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

export function useAudit(target: string | null, size: number) {
  const toast = useToast();
  const [records, setRecords] = useState<AuditRecord[]>([]);
  const [latest, setLatest] = useState<number | null>(null);
  const [loading, setLoading] = useState(false);

  const targetRef = useRef<string | null>(target);
  const generation = useRef(0);
  const recordsRef = useRef<AuditRecord[]>([]);
  const olderBusy = useRef(false);

  const commit = useCallback((next: AuditRecord[]) => {
    recordsRef.current = next;
    setRecords(next);
  }, []);

  const loadNewest = useCallback(async () => {
    const gen = ++generation.current;
    // Bu çağrı hâlâ geçerli mi? (hedef ve nesil aynı)
    const isCurrent = () => targetRef.current === target && generation.current === gen;
    olderBusy.current = false;
    if (!target) {
      commit([]);
      setLatest(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    try {
      const head = await bridge.auditList(target, 0, 1);
      if (!isCurrent()) return;
      const latestSeq = Number(head.latest_seq);
      setLatest(latestSeq);
      if (!Number.isFinite(latestSeq) || latestSeq <= 0) {
        commit([]);
        return;
      }
      const w = newestWindow(latestSeq, size);
      const page = await bridge.auditList(target, w.afterSeq, w.limit);
      if (!isCurrent()) return;
      commit(mergeNewestFirst([], page.records));
    } catch (err) {
      if (isCurrent()) toast("toast.audit_failed", { error: errorText(err) });
    } finally {
      if (isCurrent()) setLoading(false);
    }
  }, [target, size, toast, commit]);

  const loadOlder = useCallback(async () => {
    const shown = recordsRef.current;
    const oldest = shown.length > 0 ? Number(shown[shown.length - 1]!.seq) : 0;
    const w = olderWindow(oldest, AUDIT_PAGE);
    if (!target || !w || olderBusy.current) return;
    const gen = generation.current;
    const isCurrent = () => targetRef.current === target && generation.current === gen;
    olderBusy.current = true;
    setLoading(true);
    try {
      const page = await bridge.auditList(target, w.afterSeq, w.limit);
      if (!isCurrent()) return;
      commit(mergeNewestFirst(recordsRef.current, page.records));
    } catch (err) {
      if (isCurrent()) toast("toast.audit_failed", { error: errorText(err) });
    } finally {
      if (isCurrent()) {
        olderBusy.current = false;
        setLoading(false);
      }
    }
  }, [target, toast, commit]);

  // Hedef değişince önceki sunucunun kayıtları hemen düşer, sonra yenisi yüklenir.
  // targetRef önce güncellenir: uçuştaki eski çağrılar bundan sonra bayat sayılır.
  useEffect(() => {
    targetRef.current = target;
    commit([]);
    setLatest(null);
    void loadNewest();
  }, [target, size, loadNewest, commit]);

  const last = records.length > 0 ? Number(records[records.length - 1]!.seq) : 0;
  return { records, latest, loading, hasOlder: last > 1, loadNewest, loadOlder };
}

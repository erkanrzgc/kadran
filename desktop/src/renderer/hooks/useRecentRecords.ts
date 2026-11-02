/**
 * Bütün sunucuların en yeni denetim kayıtları. Sayfalama useAudit ile aynı
 * kural: önce latest_seq, sonra en yeni pencere. Okunamayan sunucu "kayıt yok"
 * sayılmaz, adı `failed` listesinde döner.
 */

import { useEffect, useState } from "react";

import { bridge, type Profile } from "../bridge.ts";
import { newestPageIntact, newestWindow, parseLatestSeq } from "../lib/audit-window.ts";
import type { ServerRecords } from "../lib/fleet.ts";

const POLL_MS = 60_000;

async function newest(target: string, size: number): Promise<ServerRecords> {
  const head = await bridge.auditList(target, 0, 1);
  const latest = parseLatestSeq(head.latest_seq);
  if (latest === null) throw new Error("server sent an invalid latest_seq");
  if (latest === 0) return { target, records: [] };
  const w = newestWindow(latest, size);
  const page = await bridge.auditList(target, w.afterSeq, w.limit);
  if (!newestPageIntact(latest, page.records)) throw new Error(`server did not return audit record ${latest}`);
  return { target, records: page.records };
}

export interface RecentRecords {
  servers: ServerRecords[];
  failed: string[];
  loaded: boolean;
}

export function useRecentRecords(profiles: readonly Profile[], active: boolean, size: number): RecentRecords {
  const [state, setState] = useState<RecentRecords>({ servers: [], failed: [], loaded: false });
  const targets = profiles.map((p) => p.target).join("\n");

  useEffect(() => {
    if (!active) return;
    let cancelled = false;
    // Geç biten eski tur yeni turun üstüne yazmasın.
    let lastRound = 0;
    const list = targets ? targets.split("\n") : [];
    // Sunucu listesi değişti: cevap gelmeden "işlem yok" denmesin.
    setState((prev) => ({ ...prev, loaded: false }));
    const round = async () => {
      if (document.hidden) return;
      const id = ++lastRound;
      const results = await Promise.allSettled(list.map((t) => newest(t, size)));
      if (cancelled || id !== lastRound) return;
      setState({
        servers: results.flatMap((r) => (r.status === "fulfilled" ? [r.value] : [])),
        failed: list.filter((_, i) => results[i]!.status === "rejected"),
        loaded: true,
      });
    };
    void round();
    const timer = window.setInterval(() => void round(), POLL_MS);
    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [active, targets, size]);

  return state;
}

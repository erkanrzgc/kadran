/**
 * Filo kartlarının ölçerleri (K-166): CPU, bellek ve disk yüzdesi. Filo
 * ekranı açıkken ve pencere görünürken intervalMs'de bir; uygulamalar
 * istenmez, yürütücüye gidilmez. Her sunucuya aynı anda tek istek.
 *
 * Hata bildirim olarak fırlatılmaz: kartın kendi durumu (useFleet) zaten
 * bağlantı sorununu gösteriyor. Ölçülemeyen sunucunun ölçeri görünmez;
 * eski sunucu bir kez anlaşılır ve bir daha sorulmaz.
 *
 * Önceki ölçümler hedef listesinden bağımsız tutulur: bir kart hazır olunca
 * liste değişir, ötekilerin CPU'su bir tur "—"ya düşmesin.
 */

import { useEffect, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { percentUsed, rates, resourceFailure, type ResourceSnapshot } from "../lib/resources.ts";

export interface FleetMeter {
  cpu: number | null;
  memory: number | null;
  disk: number | null;
}

export function useFleetResources(targets: readonly string[], intervalMs: number): ReadonlyMap<string, FleetMeter> {
  const [meters, setMeters] = useState<ReadonlyMap<string, FleetMeter>>(new Map());
  const key = targets.join("\n");
  const prevRef = useRef(new Map<string, ResourceSnapshot>());
  const unsupportedRef = useRef(new Set<string>());

  useEffect(() => {
    const list = key ? key.split("\n") : [];
    const prev = prevRef.current;
    const unsupported = unsupportedRef.current;
    const busy = new Set<string>();
    let stopped = false;
    for (const t of [...prev.keys()]) if (!list.includes(t)) prev.delete(t);

    const update = (target: string, meter: FleetMeter | null) =>
      setMeters((m) => {
        const next = new Map(m);
        if (meter) next.set(target, meter);
        else next.delete(target);
        return next;
      });

    const poll = async (target: string) => {
      busy.add(target);
      try {
        const snap = await bridge.resources(target, false);
        if (stopped) return;
        const r = rates(prev.get(target) ?? null, snap);
        prev.set(target, snap);
        const h = snap.host;
        update(target, { cpu: r.cpu, memory: percentUsed(h.memoryTotal, h.memoryAvailable), disk: percentUsed(h.diskTotal, h.diskAvailable) });
      } catch (err) {
        if (stopped) return;
        if (resourceFailure(err) === "unsupported") unsupported.add(target);
        prev.delete(target);
        update(target, null);
      } finally {
        busy.delete(target);
      }
    };
    const tick = () => {
      if (stopped || document.hidden) return;
      for (const t of list) if (!busy.has(t) && !unsupported.has(t)) void poll(t);
    };

    setMeters((m) => new Map([...m].filter(([t]) => list.includes(t))));
    tick();
    const timer = window.setInterval(tick, intervalMs);
    document.addEventListener("visibilitychange", tick);
    return () => {
      stopped = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", tick);
    };
  }, [key, intervalMs]);

  return meters;
}

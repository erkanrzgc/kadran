/**
 * Sunucu sayfasının canlı kaynak verisi (K-166). Ekran açıkken ve pencere
 * görünürken intervalMs'de bir yoklar; aynı anda tek istek gider. Hızlar
 * iki ölçümün farkından (lib/resources.ts).
 *
 * Eski sunucu (RPC yok) bir kez "unsupported" olur ve yoklama durur: her
 * turda aynı hatayı almanın anlamı yok.
 */

import { useEffect, useState } from "react";

import { bridge } from "../bridge.ts";
import { emptyHistory, pushHistory, rates, resourceFailure, type ResourceHistory, type ResourceRates, type ResourceSnapshot } from "../lib/resources.ts";

export type ResourceState = "loading" | "live" | "unsupported" | "failed";

export interface ResourceView {
  snapshot: ResourceSnapshot | null;
  rates: ResourceRates | null;
  history: ResourceHistory;
  state: ResourceState;
}

function initial(): ResourceView {
  return { snapshot: null, rates: null, history: emptyHistory(), state: "loading" };
}

export function useResources(target: string | null, intervalMs: number, includeApps: boolean): ResourceView {
  const [view, setView] = useState<ResourceView>(initial);

  useEffect(() => {
    setView(initial());
    if (!target) return;
    let prev: ResourceSnapshot | null = null;
    let busy = false;
    let stopped = false;

    const tick = async () => {
      if (busy || stopped || document.hidden) return;
      busy = true;
      try {
        const next = await bridge.resources(target, includeApps);
        if (stopped) return;
        const r = rates(prev, next);
        prev = next;
        setView((v) => ({ snapshot: next, rates: r, history: pushHistory(v.history, next, r), state: "live" }));
      } catch (err) {
        if (stopped) return;
        const failure = resourceFailure(err);
        setView((v) => ({ ...v, state: failure }));
        if (failure === "unsupported") stop();
      } finally {
        busy = false;
      }
    };
    const onVisible = () => {
      if (!document.hidden) void tick();
    };
    const timer = window.setInterval(() => void tick(), intervalMs);
    const stop = () => {
      stopped = true;
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
    document.addEventListener("visibilitychange", onVisible);
    void tick();
    return stop;
  }, [target, intervalMs, includeApps]);

  return view;
}

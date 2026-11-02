/**
 * Filo kartlarının verisi: kayıtlı her sunucudan status, apps.list, alarms.list.
 *
 * Filo ekranı görünürken 15 sn'de bir, sunucu sekmesindeyken 60 sn'de bir
 * (sekme noktaları ve alarm sayısı bayatlamasın), pencere gizliyken hiç. Aynı
 * sunucuya aynı anda tek tur gider (lib/single-flight.ts). Hata bildirim olarak
 * fırlatılmaz, karta yazılır: bağlanamayan bir sunucu ekrana bildirim yağdırmasın.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { bridge, type Profile } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { cardOf, type CardData, type FleetCard } from "../lib/fleet.ts";
import { createSingleFlight } from "../lib/single-flight.ts";

const FAST_MS = 15_000;
const SLOW_MS = 60_000;

export function useFleet(profiles: readonly Profile[], foreground: boolean): FleetCard[] {
  const [data, setData] = useState<ReadonlyMap<string, CardData>>(new Map());
  const [flight] = useState(() => createSingleFlight<void>());
  const latest = useRef(profiles);
  latest.current = profiles;

  const load = useCallback(
    (target: string) =>
      flight(target, async () => {
        const [status, apps, alarms] = await Promise.allSettled([bridge.status(target), bridge.appsList(target), bridge.alarmsList(target)]);
        const next: CardData =
          status.status === "rejected"
            ? { failure: errorText(status.reason) }
            : { status: status.value, ...(apps.status === "fulfilled" ? { apps: apps.value } : {}), ...(alarms.status === "fulfilled" ? { alarms: alarms.value } : {}) };
        setData((prev) => new Map(prev).set(target, next));
      }),
    [flight],
  );

  const targets = profiles.map((p) => p.target).join("\n");
  useEffect(() => {
    const refresh = () => {
      if (document.hidden) return;
      for (const p of latest.current) void load(p.target);
    };
    refresh();
    const timer = window.setInterval(refresh, foreground ? FAST_MS : SLOW_MS);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [foreground, targets, load]);

  return useMemo(() => profiles.map((p) => cardOf(p, data.get(p.target) ?? {})), [profiles, data]);
}

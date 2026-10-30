/**
 * Filo kartlarının verisi: kayıtlı her sunucudan status, apps.list, alarms.list.
 *
 * Yoklama yalnız filo ekranı görünürken: 15 sn'de bir, pencere gizliyken hiç.
 * Aynı sunucuya aynı anda tek tur gider (lib/single-flight.ts). Hata bildirim
 * olarak fırlatılmaz, karta yazılır: bağlanamayan bir sunucu her 15 sn'de
 * ekrana bildirim düşürmesin.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { bridge, type Profile } from "../bridge.ts";
import { errorText } from "../lib/errors.ts";
import { cardOf, type CardData, type FleetCard } from "../lib/fleet.ts";
import { createSingleFlight } from "../lib/single-flight.ts";

const POLL_MS = 15_000;

export function useFleet(profiles: readonly Profile[], active: boolean): { cards: FleetCard[]; refresh: () => void } {
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

  const refresh = useCallback(() => {
    for (const p of latest.current) void load(p.target);
  }, [load]);

  const targets = profiles.map((p) => p.target).join("\n");
  useEffect(() => {
    if (!active) return;
    refresh();
    const timer = window.setInterval(() => {
      if (!document.hidden) refresh();
    }, POLL_MS);
    const onVisible = () => {
      if (!document.hidden) refresh();
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", onVisible);
    };
  }, [active, targets, refresh]);

  const cards = useMemo(() => profiles.map((p) => cardOf(p, data.get(p.target) ?? {})), [profiles, data]);
  return { cards, refresh };
}

/**
 * Etkin sunucunun filo kartı düşünce ya da düzelince durumu bir kez yeniler
 * (R6, lib/fleet-follow.ts). Yenileme tek uçuşlu; başarısızsa useServer
 * bayat işaretler ve tek bildirim gösterir.
 */

import { useEffect, useRef } from "react";

import type { FleetCard } from "../lib/fleet.ts";
import { fleetFailed, shouldRefresh, type FleetReading } from "../lib/fleet-follow.ts";

export function useFleetFollow(cards: readonly FleetCard[], target: string | null, refresh: () => Promise<boolean>): void {
  const failed = fleetFailed(cards, target);
  const last = useRef<FleetReading>({ target: null, failed: null });

  useEffect(() => {
    const before = last.current;
    const now = { target, failed };
    last.current = now;
    if (shouldRefresh(before, now)) void refresh();
  }, [target, failed, refresh]);
}

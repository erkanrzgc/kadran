/**
 * Filo yoklaması etkin sunucuyu da okuyor (R6). Oradaki status düşerse
 * (oturum bitti, bağlantı koptu) ya da düzelirse, sunucu sayfasının durumu
 * BİR kez yenilenir. Bayatlığın tek kaynağı yine useServer'ın status
 * yenilemesi: burada yalnız ne zaman yenileneceğine karar verilir.
 */

import type { FleetCard } from "./fleet.ts";

export interface FleetReading {
  target: string | null;
  /** null: hedef yok, kart yok ya da ilk okuma gelmedi. */
  failed: boolean | null;
}

export function fleetFailed(cards: readonly FleetCard[], target: string | null): boolean | null {
  if (target === null) return null;
  const card = cards.find((c) => c.profile.target === target);
  if (!card || card.state === "loading") return null;
  return card.failure !== null;
}

/** Yalnız aynı sunucuda bilinen bir okumadan ters yöndeki bilinen okumaya geçiş. */
export function shouldRefresh(before: FleetReading, now: FleetReading): boolean {
  return (
    now.target !== null &&
    before.target === now.target &&
    before.failed !== null &&
    now.failed !== null &&
    before.failed !== now.failed
  );
}

/**
 * Üst çubuktaki sağlık hapı.
 *
 * Öncelik: daemon root çalışıyorsa kurulum bozuk; bir zincir BROKEN ise
 * kurcalama şüphesi (Attention'ı her zaman ezer); executor kapalıysa ya
 * da bir zincir doğrulanamıyorsa ilgi gerekiyor; ikisi de VALID ise sağlıklı.
 */

import type { Tone } from "./chain.ts";

export type Health = "unknown" | "healthy" | "attention" | "tampering" | "broken-install";

export interface HealthStatusInput {
  running_as_user?: string;
  executor_reachable?: boolean;
}

export interface HealthVerifyInput {
  daemon_status?: string;
  executor_status?: string;
}

/**
 * stale: son durum yenilemesi başarısız oldu, ekrandaki değerler eski.
 * Bayat veri iyi haber taşıyamaz (healthy/unknown → attention); kötü
 * haber (tampering, broken-install) bayat da olsa geçerli kalır.
 */
export function healthOf(
  status: HealthStatusInput | null,
  verify: HealthVerifyInput | null,
  stale = false,
): Health {
  const h = freshHealth(status, verify);
  if (!stale || !status) return h;
  return h === "healthy" || h === "unknown" ? "attention" : h;
}

function freshHealth(status: HealthStatusInput | null, verify: HealthVerifyInput | null): Health {
  if (!status) return "unknown";
  if (status.running_as_user === "root") return "broken-install";
  if (
    verify &&
    (verify.daemon_status === "CHAIN_STATUS_INVALID" || verify.executor_status === "CHAIN_STATUS_INVALID")
  ) {
    return "tampering";
  }
  if (!status.executor_reachable) return "attention";
  if (!verify) return "unknown";
  if (verify.daemon_status !== "CHAIN_STATUS_VALID" || verify.executor_status !== "CHAIN_STATUS_VALID") {
    return "attention";
  }
  return "healthy";
}

const TONE: Readonly<Record<Health, Tone>> = {
  unknown: "muted",
  healthy: "ok",
  attention: "warn",
  tampering: "bad",
  "broken-install": "bad",
};

export function healthTone(h: Health): Tone {
  return TONE[h];
}

export function healthKey(h: Health): string {
  return `health.${h.replace("-", "_")}`;
}

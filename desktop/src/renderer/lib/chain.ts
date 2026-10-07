/**
 * Zincir ve sonuç etiketleri. CLI ile aynı (cmd/kadran/format.go):
 * UNREACHABLE "UNVERIFIABLE" olarak görünür, "BROKEN" değil.
 */

import type { AuditRecord } from "./types.ts";

export type Tone = "ok" | "warn" | "bad" | "muted";

export interface ChainView {
  label: "VALID" | "BROKEN" | "UNVERIFIABLE" | "UNKNOWN";
  tone: Tone;
}

export function chainView(status: string | null | undefined): ChainView {
  switch (status) {
    case "CHAIN_STATUS_VALID":
      return { label: "VALID", tone: "ok" };
    case "CHAIN_STATUS_INVALID":
      return { label: "BROKEN", tone: "bad" };
    case "CHAIN_STATUS_UNREACHABLE":
      return { label: "UNVERIFIABLE", tone: "warn" };
    default:
      return { label: "UNKNOWN", tone: "muted" };
  }
}

export type OutcomeKey = "success" | "failure" | "denied" | "unspecified";
export const OUTCOME_KEYS: readonly OutcomeKey[] = ["success", "denied", "failure"];

export function outcomeKey(outcome: string | null | undefined): OutcomeKey {
  switch (outcome) {
    case "AUDIT_OUTCOME_SUCCESS":
      return "success";
    case "AUDIT_OUTCOME_FAILURE":
      return "failure";
    case "AUDIT_OUTCOME_DENIED":
      return "denied";
    default:
      return "unspecified";
  }
}

const OUTCOME_VIEW: Readonly<Record<OutcomeKey, { label: "SUCCESS" | "FAILED" | "DENIED" | "unspecified"; tone: Tone }>> = {
  success: { label: "SUCCESS", tone: "ok" },
  failure: { label: "FAILED", tone: "bad" },
  denied: { label: "DENIED", tone: "warn" },
  unspecified: { label: "unspecified", tone: "muted" },
};

export function outcomeView(outcome: string | null | undefined) {
  return OUTCOME_VIEW[outcomeKey(outcome)];
}

export const OUTCOME_TONE: Readonly<Record<OutcomeKey, Tone>> = {
  success: "ok",
  failure: "bad",
  denied: "warn",
  unspecified: "muted",
};

export function countOutcomes(records: readonly AuditRecord[]): Record<OutcomeKey, number> {
  const counts: Record<OutcomeKey, number> = { success: 0, failure: 0, denied: 0, unspecified: 0 };
  for (const r of records) counts[outcomeKey(r.outcome)] += 1;
  return counts;
}

export interface RecordFilter {
  action: string;
  outcome: OutcomeKey | "";
}

export function filterRecords(records: readonly AuditRecord[], filter: RecordFilter): AuditRecord[] {
  return records.filter(
    (r) =>
      (filter.action === "" || r.action === filter.action) &&
      (filter.outcome === "" || outcomeKey(r.outcome) === filter.outcome),
  );
}

const RANK: Readonly<Record<Tone, number>> = { ok: 0, muted: 1, warn: 2, bad: 3 };

/** worstTone, birden çok durumun en kötüsünü verir (bad > warn > muted > ok). */
export function worstTone(...tones: Tone[]): Tone {
  return tones.reduce<Tone>((worst, t) => (RANK[t] > RANK[worst] ? t : worst), "ok");
}

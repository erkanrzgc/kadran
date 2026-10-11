import type { VerifyPayload } from "./types.ts";
export const VERIFICATION_MAX_AGE_MS = 5 * 60_000;
export function verificationFailed(previous: VerifyPayload | null): VerifyPayload | null {
  // A failed read cannot clear evidence of a broken chain, or keep a previous green result current.
  return previous?.daemon_status === "CHAIN_STATUS_INVALID" || previous?.executor_status === "CHAIN_STATUS_INVALID" ? previous : null;
}
export function verificationExpired(verifiedAt: number | null, now: number): boolean {
  return verifiedAt === null || now - verifiedAt >= VERIFICATION_MAX_AGE_MS;
}

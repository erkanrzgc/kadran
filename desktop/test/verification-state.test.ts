import { test } from "node:test";
import assert from "node:assert/strict";
import { verificationFailed, verificationExpired } from "../src/renderer/lib/verification-state.ts";
import { healthOf } from "../src/renderer/lib/health.ts";
import type { VerifyPayload } from "../src/renderer/lib/types.ts";
const valid: VerifyPayload = { daemon_status: "CHAIN_STATUS_VALID", executor_status: "CHAIN_STATUS_VALID", records_checked: "4", first_invalid_seq: "0", detail: "", executor_records_checked: "2", executor_detail: "" };
test("failed verification revokes previous green state", () => {
  const next = verificationFailed(valid);
  assert.equal(next, null);
  assert.equal(healthOf({ running_as_user: "kadran", executor_reachable: true }, next, true), "attention");
});
test("failed verification retains previously observed tampering", () => {
  const broken = { ...valid, daemon_status: "CHAIN_STATUS_INVALID" as const };
  assert.equal(verificationFailed(broken), broken);
});
test("verification expires after five minutes and missing timestamp is unknown", () => {
  assert.equal(verificationExpired(null, 999), true);
  assert.equal(verificationExpired(1000, 300999), false);
  assert.equal(verificationExpired(1000, 301000), true);
});

/**
 * Zincir ve sonuç etiketleri CLI ile aynı (cmd/kadran/format.go).
 * UNREACHABLE "BROKEN" değil "UNVERIFIABLE": biri kurcalama şüphesi,
 * öteki işletim sorunu; aynı görünürlerse operatör sahte alarma koşar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { chainView, countOutcomes, filterRecords, outcomeKey, outcomeView, worstTone } from "../src/renderer/lib/chain.ts";
import type { AuditRecord } from "../src/renderer/lib/types.ts";

function rec(seq: number, action: string, outcome: AuditRecord["outcome"]): AuditRecord {
  return { seq: String(seq), ts: null, actor: null, action, target: "", params_json: "", outcome, detail: "", source: "" };
}

test("chainView etiket ve tonları", () => {
  assert.deepEqual(chainView("CHAIN_STATUS_VALID"), { label: "VALID", tone: "ok" });
  assert.deepEqual(chainView("CHAIN_STATUS_INVALID"), { label: "BROKEN", tone: "bad" });
  assert.deepEqual(chainView("CHAIN_STATUS_UNREACHABLE"), { label: "UNVERIFIABLE", tone: "warn" });
  assert.deepEqual(chainView("CHAIN_STATUS_UNSPECIFIED"), { label: "UNKNOWN", tone: "muted" });
  assert.deepEqual(chainView(undefined), { label: "UNKNOWN", tone: "muted" });
});

test("BROKEN ile UNVERIFIABLE farklı ton", () => {
  assert.notEqual(chainView("CHAIN_STATUS_INVALID").tone, chainView("CHAIN_STATUS_UNREACHABLE").tone);
});

test("sonuç anahtarı ve görünümü", () => {
  assert.equal(outcomeKey("AUDIT_OUTCOME_SUCCESS"), "success");
  assert.equal(outcomeKey("AUDIT_OUTCOME_FAILURE"), "failure");
  assert.equal(outcomeKey("AUDIT_OUTCOME_DENIED"), "denied");
  assert.equal(outcomeKey("garip"), "unspecified");
  assert.deepEqual(outcomeView("AUDIT_OUTCOME_FAILURE"), { label: "FAILED", tone: "bad" });
  assert.deepEqual(outcomeView("AUDIT_OUTCOME_DENIED"), { label: "DENIED", tone: "warn" });
});

test("countOutcomes ve filterRecords", () => {
  const records = [
    rec(3, "deploy", "AUDIT_OUTCOME_SUCCESS"),
    rec(2, "app.delete", "AUDIT_OUTCOME_DENIED"),
    rec(1, "deploy", "AUDIT_OUTCOME_FAILURE"),
  ];
  assert.deepEqual(countOutcomes(records), { success: 1, failure: 1, denied: 1, unspecified: 0 });
  assert.deepEqual(filterRecords(records, { action: "deploy", outcome: "" }).map((r) => r.seq), ["3", "1"]);
  assert.deepEqual(filterRecords(records, { action: "", outcome: "denied" }).map((r) => r.seq), ["2"]);
  assert.equal(filterRecords(records, { action: "", outcome: "" }).length, 3);
});

test("worstTone: bad > warn > muted > ok", () => {
  assert.equal(worstTone("ok", "warn"), "warn");
  assert.equal(worstTone("warn", "bad", "ok"), "bad");
  assert.equal(worstTone("ok", "muted"), "muted");
  assert.equal(worstTone("ok"), "ok");
});

/**
 * Fikstürlerin, arayüzün beklediği tel biçimini taşıdığını doğrular.
 * Fikstürleri Go testi gerçek protoToJSON çıktısından üretiyor
 * (cmd/kadran/sidecar_fixtures_test.go); bu test TS tiplerinin o biçimle
 * aynı varsayımı yaptığını ölçer.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { parseResources } from "../src/renderer/lib/resources.ts";

const here = dirname(fileURLToPath(import.meta.url));
async function fixture(name: string): Promise<Record<string, unknown>> {
  return JSON.parse(await readFile(join(here, "fixtures", name), "utf8")) as Record<string, unknown>;
}

test("status: 64 bit sayılar metin, false alan yazılmış", async () => {
  const s = await fixture("status.json");
  assert.equal(typeof s.daemon_uptime_seconds, "string");
  assert.equal(s.executor_reachable, false);
  const host = s.host as Record<string, unknown>;
  assert.equal(typeof host.memory_total_bytes, "string");
  assert.equal(typeof host.memory_available_bytes, "string");
  assert.equal(typeof host.cpu_count, "number");
});

test("verify: enum adla, sayaç metin", async () => {
  const v = await fixture("verify.json");
  assert.equal(v.daemon_status, "CHAIN_STATUS_VALID");
  assert.equal(v.executor_status, "CHAIN_STATUS_UNREACHABLE");
  assert.equal(v.records_checked, "1284");
  assert.equal(v.first_invalid_seq, "0");
});

test("audit kaydı: seq metin, zaman RFC 3339, sonuç adla", async () => {
  const r = await fixture("audit-record.json");
  assert.equal(r.seq, "1");
  assert.match(String(r.ts), /^2026-10-06T18:42:10Z$/);
  assert.equal(r.outcome, "AUDIT_OUTCOME_SUCCESS");
  assert.equal((r.actor as Record<string, unknown>).source_ip, "");
});

test("kaynak: 64 bit sayaç metin, optional sıfır yazılmış, arayüz ayrıştırıyor", async () => {
  const r = await fixture("resources.json");
  assert.equal(typeof r.sampled_at_unix_ms, "string");
  const host = r.host as Record<string, unknown>;
  assert.equal(typeof host.cpu_total_ticks, "string");
  assert.equal(host.swap_total_bytes, "0");
  assert.equal(host.memory_pressure, 0);
  assert.equal(typeof host.tasks, "number");
  const snap = parseResources(r);
  assert.equal(snap.host.swapTotal, 0);
  assert.equal(snap.host.memoryPressure, 0);
  assert.deepEqual(snap.apps?.map((a) => a.appId), ["hello", "worker"]);
});

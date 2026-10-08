#!/usr/bin/env node
/**
 * Sahte `kadran sidecar`: uçtan uca testte gerçek sunucunun yerine geçer.
 *
 * Yanıtlar elle yazılmıyor: status.json, verify.json ve audit-record.json
 * gerçek protoToJSON çıktısı (cmd/kadran/sidecar_fixtures_test.go). Audit
 * listesi GERÇEK sayfalama kuralını uygular: after_seq HARİÇ, artan sıra,
 * limit 0 → 100, üst sınır 1000 (internal/store/store.go). Böylece "en
 * eski 50'yi son kayıt diye göstermek" hatası burada da görünür.
 *
 * Özel hedefler:
 * - `fake@down`: uzun, İngilizce bir sunucu hatası döner (Türkçe arayüzde
 *   de olduğu gibi görünmeli).
 * - `fake@badseq`: audit.list bozuk bir latest_seq döner; arayüz bunu
 *   "zincirde kayıt yok" diye DEĞİL hata diye göstermeli.
 * - `fake@flaky`: ilk status çağrısı başarılı, sonrakiler hata; arayüz
 *   bayat veriyi "bağlantı koptu" diye işaretlemeli. Bu hedef SAĞLIKLI
 *   başlar (executor erişilebilir, iki zincir VALID): bayatlığın "Healthy"
 *   hapını düşürdüğü ancak böyle ölçülebilir.
 */

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = (name) => JSON.parse(readFileSync(join(here, name), "utf8"));
const status = load("status.json");
const verify = load("verify.json");
const template = load("audit-record.json");
const healthyStatus = { ...status, executor_reachable: true };
const healthyVerify = { ...verify, executor_status: "CHAIN_STATUS_VALID" };

export const LATEST = 1284;
export const DOWN_MESSAGE =
  "could not connect to fake@down: ssh: connect to host down port 22: Connection refused; " +
  "check that the server is running and that this key is allowed in /home/kadran-client/.ssh/authorized_keys";
export const FLAKY_MESSAGE = "could not reach fake@flaky: connection reset by peer";

const ACTIONS = ["deploy", "app.update", "rollback", "backup.create", "app.delete"];
const OUTCOMES = ["AUDIT_OUTCOME_SUCCESS", "AUDIT_OUTCOME_SUCCESS", "AUDIT_OUTCOME_SUCCESS", "AUDIT_OUTCOME_FAILURE", "AUDIT_OUTCOME_DENIED"];

function record(seq) {
  const ts = new Date(Date.UTC(2026, 9, 6, 18, 0, 0) - (LATEST - seq) * 60_000).toISOString().replace(".000Z", "Z");
  return { ...template, seq: String(seq), ts, action: ACTIONS[seq % 5], outcome: OUTCOMES[seq % 5] };
}

function auditList(params) {
  if (params.target === "fake@badseq") return { records: [], latest_seq: "abc" };
  const after = Number(params.after_seq ?? 0);
  const raw = Number(params.limit ?? 0);
  const limit = raw <= 0 ? 100 : Math.min(raw, 1000);
  const records = [];
  for (let s = after + 1; s <= LATEST && records.length < limit; s++) records.push(record(s));
  return { records, latest_seq: String(LATEST) };
}

// fake@flaky için status çağrı sayacı (süreç başına).
let flakyStatusCalls = 0;

const rl = createInterface({ input: process.stdin });
rl.on("line", (line) => {
  let req;
  try {
    req = JSON.parse(line);
  } catch {
    return;
  }
  const reply = (body) => process.stdout.write(JSON.stringify({ jsonrpc: "2.0", id: req.id, ...body }) + "\n");
  const params = req.params ?? {};
  if (params.target === "fake@down") {
    reply({ error: { code: -32000, message: DOWN_MESSAGE } });
    return;
  }
  switch (req.method) {
    case "version":
      reply({ result: { version: "0.5.1", commit: "fake", protocol: 3 } });
      return;
    case "status":
      if (params.target === "fake@flaky") {
        if (++flakyStatusCalls > 1) reply({ error: { code: -32000, message: FLAKY_MESSAGE } });
        else reply({ result: healthyStatus });
        return;
      }
      reply({ result: status });
      return;
    case "audit.list":
      reply({ result: auditList(params) });
      return;
    case "audit.verify":
      reply({ result: params.target === "fake@flaky" ? healthyVerify : verify });
      return;
    default:
      reply({ error: { code: -32601, message: `unknown method "${req.method}"` } });
  }
});
rl.on("close", () => process.exit(0));

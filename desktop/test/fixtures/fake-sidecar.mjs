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
 * - `fake@noalarms`: alarm listesi okunamaz, gerisi çalışır; arayüz "alarm yok"
 *   dememeli.
 * - `fake@root`: daemon root ile çalışıyor (bozuk kurulum).
 * - `fake@noalarms` kaynak çağrısını da tanımaz (eski sunucu, K-166): arayüz
 *   bunu hata değil "sunucuyu yükseltin" diye göstermeli.
 * - `fake@totp…`: sunucunun gerçek TOTP mesajını döner; kart "erişilemiyor"
 *   değil "TOTP girişi gerekli" demeli.
 * - `fake@flaky…`: zincir doğrulanana (bağlanılana) dek status başarılı, sonra
 *   hata; filo yoklaması bağlantıyı tüketmesin diye çağrı sayılmaz. Arayüz
 *   bayat veriyi "bağlantı koptu" diye işaretlemeli. Bu hedef SAĞLIKLI
 *   başlar (executor erişilebilir, iki zincir VALID): bayatlığın "Healthy"
 *   hapını düşürdüğü ancak böyle ölçülebilir.
 */

import { readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const load = (name) => JSON.parse(readFileSync(join(here, name), "utf8"));
const status = load("status.json");
const verify = load("verify.json");
const resourcesBase = load("resources.json");
const template = load("audit-record.json");
// Hata biçimi gerçek sidecar'dan: internal/sidecar/error_test.go bu altın dosyayı üretir/denetler.
// `message` sabit sınıf adı, gerçek sebep `data`'dadır.
const goldenError = load("sidecar-error.json");
const rpcFail = (kind, data) => ({ error: { ...goldenError[kind], data } });
const healthyStatus = { ...status, executor_reachable: true };
const healthyVerify = { ...verify, executor_status: "CHAIN_STATUS_VALID" };

export const LATEST = 1284;
export const DOWN_MESSAGE =
  "ssh: connect to host down port 22: Connection refused; " +
  "check that the server is running and that this key is allowed in /home/kadran-client/.ssh/authorized_keys";
export const TOTP_MESSAGE = 'TOTP is on for this server: sign in with "kadran login" (upgrade kadran if it has no login command)';
export const FLAKY_MESSAGE = "rpc error: code = Unavailable desc = connection reset by peer";

const ACTIONS = ["app.deploy", "app.update", "app.rollback", "backup.create", "app.delete"];
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

// Hangi fake@flaky… hedefine bağlanıldı? (süreç başına)
const flakyConnected = new Set();
let verificationCalls = 0;
let backupCalls = 0;
const resourceCalls = new Map();
const appSpec = (id) => ({ app_id: id, git_host: "github.com", git_owner: "team", git_repo: "site", git_branch: "main", dockerfile_path: "Dockerfile", container_port: 3000, replicas: 1, health_path: "/healthz", domain: `${id}.example.com`, limits: { memory_bytes: "536870912", cpu_millis: 1000, blkio_weight: 100 }, build_args: {}, env: { MODE: "old", OLD: "remove" }, volumes: [{ name: "data", mount_path: "/app/data", read_only: false }] });
const app = (spec) => ({ spec, created_at: "2026-10-10T12:00:00Z", updated_at: "2026-10-11T12:00:00Z", release_count: 2 });
// Like the real API, values are never returned to the renderer.
const publicApp = (value) => ({ ...value, spec: { ...value.spec, env: Object.fromEntries(Object.keys(value.spec.env).map((key) => [key, ""])) } });
const apps = new Map([["hello", app(appSpec("hello"))], ["draft", { ...app(appSpec("draft")), release_count: 0 }]]);
const releases = ["r2", "r1"].map((id) => ({ release_id: id, app_id: "hello", commit_sha: (id === "r2" ? "b" : "a").repeat(40), status: "RELEASE_STATUS_BUILT", image_id: "sha256:" + "c".repeat(64), started_at: "2026-10-10T12:00:00Z", finished_at: "2026-10-10T12:00:10Z", detail: "Health gate passed" }));
let activeRelease = "r2";
const backups = [{ path: "/var/lib/kadran/backups/kadran-20261010T120000Z.db", bytes: "262144", taken_unix: "1791633600" }];

// Ortak sunucu listesi (K-162): KADRAN_FAKE_SERVERS_FILE'daki dosya (smoke onu
// "CLI" gibi değiştirir), yoksa bellek. Kurallar internal/servers'ın sade hâli:
// aynı hedef ve büyük/küçük harfe bakmadan aynı ad yok, içe aktarmada çakışan
// ad "ad (2)" olur. Hata biçimi gerçek yan süreçteki gibi: sebep `data`'da.
const serversFile = process.env.KADRAN_FAKE_SERVERS_FILE;
let memoryServers = [];
function readServers() {
  if (!serversFile) return memoryServers;
  try {
    return JSON.parse(readFileSync(serversFile, "utf8")).servers ?? [];
  } catch (err) {
    if (err.code === "ENOENT") return [];
    throw err;
  }
}
function writeServers(list) {
  if (serversFile) writeFileSync(serversFile, JSON.stringify({ version: 1, servers: list }, null, 2));
  else memoryServers = list;
  return { result: { servers: list } };
}
const serverFail = (message, reason) => ({ error: { code: -32603, message, data: reason } });
const sameName = (a, b) => a.toLowerCase() === b.toLowerCase();
const entry = (name, target, provider) => ({ name, target, ...(provider ? { provider } : {}) });

function serversCall(method, params) {
  const list = readServers();
  switch (method) {
    case "servers.list":
      return { result: { servers: list } };
    case "servers.add": {
      const name = String(params.name).trim();
      if (list.some((s) => s.target === params.target)) return serverFail("a saved server already has this target", "duplicate_target");
      if (list.some((s) => sameName(s.name, name))) return serverFail("a saved server already has this name", "duplicate_name");
      return writeServers([...list, entry(name, params.target, params.provider)]);
    }
    case "servers.update": {
      const name = String(params.name).trim();
      if (!list.some((s) => s.target === params.target)) return serverFail("no saved server with this target", "not_found");
      if (list.some((s) => s.target !== params.target && sameName(s.name, name))) return serverFail("a saved server already has this name", "duplicate_name");
      // Yönetici hedefi ve sudo, gerçek yan süreçteki gibi korunur.
      return writeServers(list.map(({ provider: _old, ...s }) => (s.target === params.target ? { ...s, name, ...(params.provider ? { provider: params.provider } : {}) } : { ...s, ...(_old ? { provider: _old } : {}) })));
    }
    case "servers.remove":
      if (!list.some((s) => s.target === params.target)) return serverFail("no saved server with this target", "not_found");
      return writeServers(list.filter((s) => s.target !== params.target));
    case "servers.import": {
      const next = [...list];
      let imported = 0;
      for (const p of params.profiles ?? []) {
        if (next.some((s) => s.target === p.target)) continue;
        const base = String(p.name).trim().replace(/^[-\s]+/, "").slice(0, 64) || p.target;
        let name = base;
        for (let n = 2; next.some((s) => sameName(s.name, name)); n++) name = `${base.slice(0, 64 - ` (${n})`.length)} (${n})`;
        next.push(entry(name, p.target, p.provider));
        imported++;
      }
      return { result: { ...writeServers(next).result, imported } };
    }
  }
  return { error: { code: -32601, message: `unknown method "${method}"` } };
}

/**
 * Kaynak sayaçları (K-166): resources.json tabanından her çağrıda artar,
 * ölçüm anı her çağrıda 2 sn ilerler. Hız böylece gerçek aralıktan bağımsız:
 * CPU %35 (bekleme %1,5), "hello" %7,5, "worker" %15. Replika sayısı sabit;
 * değişseydi uygulama hızı bilinmez olurdu.
 */
function resourceUsage(params) {
  const n = (resourceCalls.get(params.target) ?? 0) + 1;
  resourceCalls.set(params.target, n);
  const bump = (value, step) => String(Number(value) + step * n);
  const h = resourcesBase.host;
  const host = {
    ...h,
    cpu_total_ticks: bump(h.cpu_total_ticks, 400),
    cpu_idle_ticks: bump(h.cpu_idle_ticks, 260),
    cpu_iowait_ticks: bump(h.cpu_iowait_ticks, 6),
    cpu_steal_ticks: bump(h.cpu_steal_ticks, 2),
    disk_read_bytes: bump(h.disk_read_bytes, 40_960),
    disk_write_bytes: bump(h.disk_write_bytes, 262_144),
    net_rx_bytes: bump(h.net_rx_bytes, 180_000),
    net_tx_bytes: bump(h.net_tx_bytes, 52_000),
    uptime_seconds: bump(h.uptime_seconds, 2),
  };
  const sampled = bump(resourcesBase.sampled_at_unix_ms, 2000);
  // Gerçek kadrand gibi: executor'a ulaşılamıyorsa (status.json, fake@test)
  // uygulamalar "ölçülemedi"; host sayaçları yine gelir.
  const executorUp = String(params.target).startsWith("fake@flaky") || ["fake@verifyfail", "fake@slow", "fake@root"].includes(params.target);
  if (!params.include_apps || !executorUp) return { sampled_at_unix_ms: sampled, host, apps: [], apps_measured: false };
  const apps = resourcesBase.apps.map((a, i) => ({
    ...a,
    cpu_usage_ns: bump(a.cpu_usage_ns, (i + 1) * 300_000_000),
    net_rx_bytes: bump(a.net_rx_bytes, 20_000),
    net_tx_bytes: bump(a.net_tx_bytes, 90_000),
    disk_write_bytes: bump(a.disk_write_bytes, i * 65_536),
  }));
  return { ...resourcesBase, sampled_at_unix_ms: sampled, host, apps };
}

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
  // Liste yöntemleri hedefe bağlanmaz: özel hedeflerin hatası onlara uygulanmaz.
  if (typeof req.method === "string" && req.method.startsWith("servers.")) {
    reply(serversCall(req.method, params));
    return;
  }
  if (params.target === "fake@down") {
    reply(rpcFail("connect", DOWN_MESSAGE));
    return;
  }
  if (typeof params.target === "string" && params.target.startsWith("fake@totp")) {
    reply(rpcFail("call", TOTP_MESSAGE));
    return;
  }
  switch (req.method) {
    case "version":
      reply({ result: { version: "0.5.1", commit: "fake", protocol: 3 } });
      return;
    case "status":
      if (String(params.target).startsWith("fake@flaky")) {
        if (flakyConnected.has(params.target)) reply(rpcFail("call", FLAKY_MESSAGE));
        else reply({ result: healthyStatus });
        return;
      }
      if (["fake@verifyfail", "fake@slow"].includes(params.target)) { reply({ result: healthyStatus }); return; }
      if (params.target === "fake@root") { reply({ result: { ...healthyStatus, running_as_user: "root" } }); return; }
      reply({ result: status });
      return;
    case "resources":
      if (params.target === "fake@noalarms") { reply(rpcFail("call", "rpc error: code = Unimplemented desc = unknown method GetResourceUsage for service kadran.v1.KadranService")); return; }
      if (flakyConnected.has(params.target)) { reply(rpcFail("call", FLAKY_MESSAGE)); return; }
      reply({ result: resourceUsage(params) });
      return;
    case "audit.list":
      reply({ result: auditList(params) });
      return;
    case "audit.verify":
      if (String(params.target).startsWith("fake@flaky")) flakyConnected.add(params.target);
      if (params.target === "fake@verifyfail" && ++verificationCalls > 1) { reply(rpcFail("call", "verification connection lost")); return; }
      reply({ result: String(params.target).startsWith("fake@flaky") || ["fake@verifyfail", "fake@slow"].includes(params.target) ? healthyVerify : verify });
      return;
    case "apps.list":
      if (params.target === "fake@slow") { setTimeout(() => reply({ result: { apps: [app(appSpec("old-only"))] } }), 1800); return; }
      reply({ result: { apps: [...apps.values()].map(publicApp) } });
      return;
    case "apps.get":
      if (!apps.has(params.app_id)) { reply(rpcFail("call", "application not found")); return; }
      reply({ result: { app: publicApp(apps.get(params.app_id)), releases: params.app_id === "hello" ? releases : [], active_release_id: params.app_id === "hello" ? activeRelease : "" } });
      return;
    case "apps.create":
      if (apps.has(params.spec.app_id)) { reply(rpcFail("call", "application already exists")); return; }
      apps.set(params.spec.app_id, { ...app(params.spec), release_count: 0 });
      reply({ result: { app: publicApp(apps.get(params.spec.app_id)) } });
      return;
    case "apps.update": {
      const current = apps.get(params.app_id);
      if (!current) { reply(rpcFail("call", "application not found")); return; }
      const { env_remove = [], volume_remove = [], env = {}, volumes = [], ...scalars } = params.update;
      current.spec = { ...current.spec, ...scalars, env: { ...current.spec.env, ...env }, volumes: [...current.spec.volumes.filter((v) => !volume_remove.includes(v.name) && !volumes.some((n) => n.name === v.name)), ...volumes] };
      for (const key of env_remove) delete current.spec.env[key];
      reply({ result: { app: publicApp(current), proxy_detail: "Domain route reconciled", env_detail: "Environment applies on next deploy", volume_detail: "" } });
      return;
    }
    case "apps.deploy":
      setTimeout(() => {
        if (params.ref === "fail") { reply(rpcFail("call", "health gate failed: HTTP 503; existing traffic retained")); return; }
        activeRelease = "r3";
        apps.get("hello").release_count = 3;
        releases.unshift({ ...releases[0], release_id: "r3", commit_sha: "d".repeat(40) });
        reply({ result: { release_id: "r3", image_id: "sha256:" + "c".repeat(64), output: "Build complete\nMerhaba ✓\nHealth gate passed\n", truncated: false } });
      }, 300);
      return;
    case "apps.rollback": {
      const previous = activeRelease;
      activeRelease = "r2";
      setTimeout(() => reply({ result: { app_id: params.app_id, from_release_id: previous, to_release_id: activeRelease, recreated: false } }), 150);
      return;
    }
    case "apps.delete": {
      if (params.app_id === "hello" && activeRelease) { reply(rpcFail("call", "application has an active release")); return; }
      const current = apps.get(params.app_id);
      apps.delete(params.app_id);
      reply({ result: { app_id: params.app_id, containers_removed: 0, releases_deleted: 0, deployments_deleted: 0, volumes_kept: (current?.spec.volumes ?? []).map((v) => v.name) } });
      return;
    }
    case "apps.logs":
      reply({ result: { output: "Merhaba ✓\n<script id=\"unsafe-log\">throw new Error('unsafe')</script>\nServing traffic on r2\n", truncated: false } });
      return;
    case "alarms.list":
      if (params.target === "fake@noalarms") { reply(rpcFail("call", "alarm store unavailable")); return; }
      reply({ result: { alarms: [{ id: "disk_low:host", kind: "disk_low", target: "host", severity: "ALARM_SEVERITY_WARNING", since_unix: "1791633600", detail: "Disk space is below the configured threshold; inspect application volumes and images." }], delivery_is_local_only: true } });
      return;
    case "backups.list":
      reply({ result: { backups, keep: 7 } });
      return;
    case "backups.create":
      setTimeout(() => {
        if (++backupCalls === 1) { reply(rpcFail("call", "backup failed: disk full")); return; }
        const backup = { path: "/var/lib/kadran/backups/kadran-20261011T120000Z.db", bytes: "266240", taken_unix: "1791720000" };
        backups.unshift(backup);
        reply({ result: { backup, volumes_excluded: true } });
      }, 150);
      return;
    default:
      reply({ error: { code: -32601, message: `unknown method "${req.method}"` } });
  }
});
rl.on("close", () => process.exit(0));

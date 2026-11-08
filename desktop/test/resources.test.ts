/**
 * Canlı kaynak kullanımı (K-166): yanıtın ayrıştırılması ve iki ölçüm
 * arasındaki hızlar. Sunucu kümülatif sayaç dönüyor; hız burada.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { parseResources, rates, type ResourceSnapshot } from "../src/renderer/lib/resources.ts";

/** Sidecar'ın ürettiği biçim: 64 bit sayılar dize, olmayan alan yok. */
function payload(over: { at?: number; host?: Record<string, unknown>; apps?: unknown[] | null } = {}): unknown {
  const host = {
    cpu_count: 2,
    cpu_total_ticks: "1000",
    cpu_idle_ticks: "800",
    cpu_iowait_ticks: "50",
    cpu_steal_ticks: "10",
    memory_total_bytes: "4000",
    memory_available_bytes: "1000",
    net_rx_bytes: "10000",
    net_tx_bytes: "20000",
    disk_read_bytes: "0",
    disk_write_bytes: "0",
    load1: 0.5,
    tasks: 120,
    ...over.host,
  };
  const apps = over.apps === undefined ? [] : over.apps;
  return {
    sampled_at_unix_ms: String(over.at ?? 1_000_000),
    host,
    apps: apps ?? [],
    apps_measured: apps !== null,
  };
}

function app(id: string, cpuNs: number, extra: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    app_id: id,
    containers: 1,
    cpu_usage_ns: String(cpuNs),
    memory_bytes: "100",
    memory_limit_bytes: "1000",
    net_rx_bytes: "0",
    net_tx_bytes: "0",
    disk_read_bytes: "0",
    disk_write_bytes: "0",
    tasks: 3,
    ...extra,
  };
}

test("parses strings to numbers and keeps missing fields null", () => {
  const s = parseResources(payload());
  assert.equal(s.at, 1_000_000);
  assert.equal(s.host.cpuCount, 2);
  assert.equal(s.host.cpuTotal, 1000);
  assert.equal(s.host.memoryTotal, 4000);
  assert.equal(s.host.load1, 0.5);
  assert.equal(s.host.tasks, 120);
  // Gönderilmeyen alan "ölçülemedi": null, 0 değil.
  assert.equal(s.host.swapTotal, null);
  assert.equal(s.host.cpuPressure, null);
  assert.deepEqual(s.apps, []);
});

test("unmeasured apps are null, not an empty list", () => {
  assert.equal(parseResources(payload({ apps: null })).apps, null);
});

test("malformed payload throws", () => {
  for (const bad of [
    null,
    "x",
    { host: {} },
    payload({ host: { cpu_total_ticks: "abc" } }),
    payload({ host: { net_rx_bytes: "-5" } }),
    payload({ host: { load1: "0.5" } }),
    payload({ apps: [{ app_id: 5 }] }),
    payload({ apps: [app("", 1)] }),
  ]) {
    assert.throws(() => parseResources(bad), /invalid resource usage/, JSON.stringify(bad));
  }
});

test("first snapshot has no rates", () => {
  const r = rates(null, parseResources(payload()));
  assert.equal(r.cpu, null);
  assert.equal(r.netRx, null);
  assert.equal(r.apps.size, 0);
});

test("cpu percent from ticks", () => {
  const a = parseResources(payload());
  const b = parseResources(payload({ at: 1_002_000, host: { cpu_total_ticks: "1200", cpu_idle_ticks: "850", cpu_iowait_ticks: "70", cpu_steal_ticks: "30" } }));
  const r = rates(a, b);
  assert.equal(r.cpu, 75); // Δtoplam 200, Δboşta 50
  assert.equal(r.iowait, 10); // 20 / 200
  assert.equal(r.steal, 10);
});

test("byte counters become per-second rates", () => {
  const a = parseResources(payload());
  const b = parseResources(payload({ at: 1_002_000, host: { net_rx_bytes: "14000", net_tx_bytes: "20000", disk_write_bytes: "4096" } }));
  const r = rates(a, b);
  assert.equal(r.netRx, 2000);
  assert.equal(r.netTx, 0);
  assert.equal(r.diskWrite, 2048);
});

test("negative delta yields null", () => {
  // Host yeniden başladı: sayaçlar sıfırlandı. Sıçrama yerine "—".
  const a = parseResources(payload());
  const b = parseResources(payload({ at: 1_002_000, host: { net_rx_bytes: "5", cpu_total_ticks: "100", cpu_idle_ticks: "90" } }));
  const r = rates(a, b);
  assert.equal(r.netRx, null);
  assert.equal(r.cpu, null);
  assert.equal(r.netTx, 0); // geri gitmeyen sayaç yine hesaplanır
});

test("zero or negative interval yields null", () => {
  const a = parseResources(payload({ at: 5000 }));
  for (const at of [5000, 4000]) {
    const r = rates(a, parseResources(payload({ at, host: { net_rx_bytes: "99999" } })));
    assert.equal(r.netRx, null);
    assert.equal(r.cpu, null);
  }
});

test("missing optional field stays null", () => {
  const a = parseResources(payload({ host: { net_rx_bytes: undefined } }));
  const b = parseResources(payload({ at: 1_002_000 }));
  assert.equal(rates(a, b).netRx, null);
});

test("app cpu is a share of the host", () => {
  // 2 çekirdek, 2 sn: hostun tamamı 4e9 ns. 1e9 ns → %25.
  const a = parseResources(payload({ apps: [app("blog", 0)] }));
  const b = parseResources(payload({ at: 1_002_000, apps: [app("blog", 1e9, { net_rx_bytes: "4000" })] }));
  const r = rates(a, b).apps.get("blog");
  assert.ok(r);
  assert.equal(r.cpu, 25);
  assert.equal(r.netRx, 2000);
});

test("replica count change makes app rates unknown", () => {
  // Yeni replika kendi kümülatif sayacını getirir: bir aralık şişerdi.
  const a = parseResources(payload({ apps: [app("blog", 0)] }));
  const b = parseResources(payload({ at: 1_002_000, apps: [app("blog", 5e9, { containers: 2 })] }));
  const r = rates(a, b).apps.get("blog");
  assert.ok(r);
  assert.equal(r.cpu, null);
  assert.equal(r.netRx, null);
});

test("new app has no rates until its second sample", () => {
  const a: ResourceSnapshot = parseResources(payload({ apps: [] }));
  const b = parseResources(payload({ at: 1_002_000, apps: [app("api", 1e9)] }));
  assert.equal(rates(a, b).apps.get("api")?.cpu ?? null, null);
});

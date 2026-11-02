/**
 * Filo mantığı: kart durumu, sağlayıcı sırası ve toplu denetim.
 * Bunlar saf; ekran doğruluğu duman yolculuğunda ayrıca ölçülür.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  alarmCount,
  appOfTarget,
  cardOf,
  classifyFailure,
  coverage,
  diskUsedPercent,
  mergeOperations,
  orderByProvider,
  summarize,
  updateAvailable,
  type FleetCard,
} from "../src/renderer/lib/fleet.ts";
import { providerView } from "../src/renderer/lib/providers.ts";
import type { AlarmsPayload, AppsListPayload, AuditRecord, StatusPayload } from "../src/renderer/lib/types.ts";

const status = (over: Partial<StatusPayload> = {}): StatusPayload => ({
  daemon_version: "v0.9.3",
  hostname: "h",
  daemon_uptime_seconds: "10",
  running_as_user: "kadran",
  executor_reachable: true,
  executor_version: "v0.9.3",
  host: { os: "Ubuntu 24.04", kernel_version: "6", architecture: "x86_64", cpu_count: 2, memory_total_bytes: "8", memory_available_bytes: "4", docker_version: "29", disk_total_bytes: "100", disk_available_bytes: "75" },
  ...over,
});
const apps = (n: number): AppsListPayload => ({ apps: Array.from({ length: n }, () => ({ spec: null as never, created_at: null, updated_at: null, release_count: 0 })) });
const alarms = (n: number): AlarmsPayload => ({ delivery_is_local_only: true, alarms: Array.from({ length: n }, (_, i) => ({ id: String(i), kind: "disk_low", target: "host", severity: "ALARM_SEVERITY_WARNING", since_unix: "1", detail: "" })) });
const profile = (target: string, provider?: string) => ({ name: target, target, ...(provider ? { provider } : {}) });
const ready = (target: string, provider?: string, alarmN = 0): FleetCard => cardOf(profile(target, provider), { status: status(), apps: apps(1), alarms: alarms(alarmN) });

test("TOTP hatası 'erişilemiyor' değil; sunucunun bütün giriş mesajları tanınır", () => {
  assert.equal(classifyFailure('TOTP is on for this server: sign in with "kadran login" (upgrade kadran if it has no login command)'), "totp");
  assert.equal(classifyFailure('session expired: sign in again with "kadran login"'), "totp");
  assert.equal(classifyFailure('invalid session token: sign in again with "kadran login"'), "totp");
  assert.equal(classifyFailure("ssh: connect to host x port 22: Connection refused"), "unreachable");
  assert.equal(classifyFailure(""), "unreachable");
});

test("adında totp geçen ulaşılamaz sunucu giriş istemiyor sayılmaz", () => {
  assert.equal(classifyFailure("ssh: connect to host totp-1 port 22: Connection refused"), "unreachable");
});

test("kart tonu: alarm ya da erişilemeyen denetçi uyarıdır, sağlıklı olmayan gizlenmez", () => {
  assert.equal(ready("a").tone, "ok");
  assert.equal(ready("a", undefined, 1).tone, "warn");
  assert.equal(cardOf(profile("a"), { status: status({ executor_reachable: false }), apps: apps(0), alarms: alarms(0) }).tone, "warn");
  assert.equal(cardOf(profile("a"), { failure: 'run "kadran login"' }).state, "totp");
  assert.equal(cardOf(profile("a"), { failure: 'run "kadran login"' }).tone, "muted");
  assert.equal(cardOf(profile("a"), { failure: "refused" }).state, "unreachable");
  assert.equal(cardOf(profile("a"), { failure: "refused" }).tone, "bad");
  assert.equal(cardOf(profile("a"), {}).state, "loading");
});

test("root ile çalışan bozuk kurulum yeşil görünmez", () => {
  const card = cardOf(profile("a"), { status: status({ running_as_user: "root" }), apps: apps(0), alarms: alarms(0) });
  assert.equal(card.tone, "bad");
  assert.deepEqual(summarize([card]), { servers: 1, ready: 0, warning: 0, signIn: 0, failed: 1, apps: 0 });
});

test("alarm listesi okunamazsa kart 'alarm yok' iddia etmez", () => {
  const card = cardOf(profile("a"), { status: status(), apps: apps(1) });
  assert.equal(card.alarmCount, null);
  assert.equal(card.tone, "warn");
  assert.equal(alarmCount([card, ready("b", undefined, 2)]), 2);
});

test("toplu liste okunamayanı adıyla söyler, yüklenirken 'yok' demez", () => {
  const noAlarms = cardOf(profile("a"), { status: status(), apps: apps(1) });
  assert.deepEqual(coverage([noAlarms, ready("b")], "alarms"), { loading: false, unread: ["a"] });
  assert.deepEqual(coverage([noAlarms, ready("b")], "apps"), { loading: false, unread: [] });
  const down = cardOf(profile("c"), { failure: "refused" });
  assert.deepEqual(coverage([down], "apps"), { loading: false, unread: ["c"] });
  assert.deepEqual(coverage([cardOf(profile("d"), {})], "apps"), { loading: true, unread: [] });
});

test("disk yüzdesi: bilinmiyorsa null, sıfıra bölmez", () => {
  assert.equal(diskUsedPercent(status().host), 25);
  assert.equal(diskUsedPercent(null), null);
  assert.equal(diskUsedPercent({ ...status().host!, disk_total_bytes: "0" }), null);
});

test("aynı sağlayıcı yan yana, ilk görülme sırasında, etiketsizler sonda", () => {
  const order = orderByProvider([ready("a"), ready("b", "hetzner"), ready("c", "google-cloud"), ready("d", "hetzner")]);
  assert.deepEqual(order.map((c) => c.profile.target), ["b", "d", "c", "a"]);
});

test("bilinmeyen sağlayıcı etiketi metin olarak kalır, renk uydurulmaz", () => {
  assert.equal(providerView("hetzner").color, "#d50c2d");
  assert.equal(providerView("hetzner").letter, "H");
  assert.equal(providerView("kendi-sunucum").name, "kendi-sunucum");
  assert.equal(providerView("kendi-sunucum").letter, "K");
  assert.equal(providerView("kendi-sunucum").color, null);
  assert.equal(providerView("").letter, "?");
});

test("elle bozulmuş sağlayıcı adı uygulamayı düşürmez", () => {
  for (const name of ["__proto__", "constructor", "toString"]) {
    assert.equal(providerView(name).name, name);
    assert.equal(providerView(name).color, null);
  }
});

test("özet yalnız okunabilenleri sayar", () => {
  const s = summarize([ready("a"), ready("b", undefined, 1), cardOf(profile("c"), { failure: "refused" }), cardOf(profile("d"), { failure: '"kadran login"' })]);
  assert.deepEqual(s, { servers: 4, ready: 1, warning: 1, signIn: 1, failed: 1, apps: 2 });
});

test("sürüm farkı: yalnız sunucu istemciden eskiyse, 'dev' derlemede susar", () => {
  assert.equal(updateAvailable("v0.9.2", "v0.9.3"), true);
  assert.equal(updateAvailable("v0.9.3", "v0.9.3"), false);
  assert.equal(updateAvailable("v0.10.0", "v0.9.3"), false);
  assert.equal(updateAvailable("v0.9.2", "dev"), false);
  assert.equal(updateAvailable("dev", "v0.9.3"), false);
});

const record = (seq: number, action: string, ts: string | null): AuditRecord => ({ seq: String(seq), ts, actor: null, action, target: "app", params_json: "{}", outcome: "AUDIT_OUTCOME_SUCCESS", detail: "", source: "" });

test("toplu denetim: bütün sunuculardan en yeni üstte", () => {
  const merged = mergeOperations(
    [
      { target: "a", records: [record(1, "app.deploy", "2026-10-30T10:00:00Z"), record(2, "app.update", "2026-10-30T11:00:00Z")] },
      { target: "b", records: [record(9, "app.rollback", "2026-10-30T12:00:00Z")] },
    ],
    5,
  );
  assert.deepEqual(merged.map((m) => [m.server, m.record.seq]), [["b", "9"], ["a", "2"], ["a", "1"]]);
});

test("toplu denetim: sınır uygulanır, zamansız kayıt sona düşer", () => {
  const merged = mergeOperations(
    [{ target: "a", records: [record(1, "x", null), record(2, "y", "2026-10-30T10:00:00Z"), record(3, "z", "2026-10-30T11:00:00Z")] }],
    2,
  );
  assert.deepEqual(merged.map((m) => m.record.seq), ["3", "2"]);
});

test("denetim hedefinden uygulama adı", () => {
  assert.equal(appOfTarget("app/blog/release/42"), "blog");
  assert.equal(appOfTarget("app/blog"), "blog");
  assert.equal(appOfTarget("host"), "host");
  assert.equal(appOfTarget(""), "");
});

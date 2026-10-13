/**
 * Biçimlendiriciler CLI ile AYNI çıktıyı vermeli (cmd/kadran/format.go):
 * kullanıcı aynı değeri iki yerde farklı okumamalı.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  describeActor,
  formatCount,
  formatTime,
  humanBytes,
  humanDuration,
  memoryText,
  memoryUsedGiB,
  shortFingerprint,
  versionLabel,
} from "../src/renderer/lib/format.ts";

test("humanDuration CLI ile aynı: en çok iki birim, d h m s", () => {
  assert.equal(humanDuration(0), "0s");
  assert.equal(humanDuration("0.5"), "0s");
  assert.equal(humanDuration(59), "59s");
  assert.equal(humanDuration(61), "1m 1s");
  assert.equal(humanDuration(3_600 * 3 + 5), "3h 5s");
  assert.equal(humanDuration("371520"), "4d 7h");
  assert.equal(humanDuration(86_400 * 3 + 300), "3d 5m");
  assert.equal(humanDuration("abc"), "0s");
});

test("humanBytes CLI ile aynı", () => {
  assert.equal(humanBytes(512), "512 B");
  assert.equal(humanBytes(1024), "1.0 KiB");
  assert.equal(humanBytes("3970000000"), "3.7 GiB");
  assert.equal(humanBytes(-1), "—");
  assert.equal(humanBytes("x"), "—");
});

test("humanBytes tam yarımda CLI gibi çifte yuvarlar", () => {
  assert.equal(humanBytes(1280), "1.2 KiB");
  assert.equal(humanBytes(2304), "2.2 KiB");
  assert.equal(humanBytes(1792), "1.8 KiB");
  assert.equal(humanBytes(1342177280), "1.2 GiB");
});

test("shortFingerprint uzun izi kısaltır, kısa izi bırakır", () => {
  assert.equal(
    shortFingerprint("SHA256:4f1c2b7d9e0a11223344556677889900aabbccddeeffa9e2"),
    "SHA256:4f1c2b7d…ffa9e2",
  );
  assert.equal(shortFingerprint("SHA256:abc"), "SHA256:abc");
});

test("describeActor: parmak izi > etiket > köken > null", () => {
  const base = { ssh_key_fingerprint: "", source_ip: "", label: "", origin: "" };
  assert.equal(describeActor(null), null);
  assert.equal(describeActor(base), null);
  assert.equal(describeActor({ ...base, origin: "ssh" }), "ssh");
  assert.equal(describeActor({ ...base, origin: "ssh", label: "ci" }), "ci");
  assert.equal(
    describeActor({ ...base, label: "ci", ssh_key_fingerprint: "SHA256:4f1c2b7d9e0a11223344556677889900aabbccddeeffa9e2" }),
    "SHA256:4f1c2b7d…ffa9e2",
  );
});

test("bellek: kullanılan GiB ve metin; eksik host güvenli", () => {
  const host = {
    os: "linux", kernel_version: "", architecture: "", cpu_count: 2,
    memory_total_bytes: "4294967296", memory_available_bytes: "1073741824",
    docker_version: "", disk_total_bytes: "0", disk_available_bytes: "0",
  };
  assert.equal(memoryUsedGiB(host), 3);
  assert.equal(memoryText(host), "3.0 GiB / 4.0 GiB");
  assert.ok(Number.isNaN(memoryUsedGiB(null)));
  assert.equal(memoryText(null), "—");
  assert.equal(memoryText({ ...host, memory_total_bytes: "0" }), "—");
});

test("formatTime ve formatCount", () => {
  assert.equal(formatTime(null, "en"), "—");
  assert.notEqual(formatTime("2026-10-06T18:42:10Z", "en"), "—");
  assert.equal(formatCount(1284, "en"), "1,284");
});

test("versionLabel: CI etiketi zaten v ile başlar, iki kez v yazılmaz", () => {
  assert.equal(versionLabel("v0.5.1"), "v0.5.1");
  assert.equal(versionLabel("0.5.1"), "v0.5.1");
  assert.equal(versionLabel("v0.5.1-47-g0c91e9c-dirty"), "v0.5.1-47-g0c91e9c-dirty");
  // Etiketsiz yerel derleme: "dev" ya da kısa commit; başına v konmaz.
  assert.equal(versionLabel("dev"), "dev");
  assert.equal(versionLabel("0c91e9c"), "0c91e9c");
});

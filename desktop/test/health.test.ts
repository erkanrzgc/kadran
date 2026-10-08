/** Sağlık hapı: root > BROKEN > ulaşılamayan executor / UNVERIFIABLE > sağlıklı. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { healthKey, healthOf, healthTone } from "../src/renderer/lib/health.ts";

const ok = { running_as_user: "kadran", executor_reachable: true };
const valid = { daemon_status: "CHAIN_STATUS_VALID", executor_status: "CHAIN_STATUS_VALID" };

test("durum yoksa bilinmiyor", () => {
  assert.equal(healthOf(null, null), "unknown");
});

test("root her şeyi ezer", () => {
  assert.equal(healthOf({ ...ok, running_as_user: "root" }, valid), "broken-install");
});

test("BROKEN her zaman Attention'ı ezer", () => {
  assert.equal(healthOf({ ...ok, executor_reachable: false }, { ...valid, executor_status: "CHAIN_STATUS_INVALID" }), "tampering");
  assert.equal(healthOf(ok, { ...valid, daemon_status: "CHAIN_STATUS_INVALID" }), "tampering");
});

test("executor kapalı ya da UNVERIFIABLE → attention", () => {
  assert.equal(healthOf({ ...ok, executor_reachable: false }, null), "attention");
  assert.equal(healthOf(ok, { ...valid, executor_status: "CHAIN_STATUS_UNREACHABLE" }), "attention");
});

test("doğrulama yoksa ve executor açıksa bilinmiyor; ikisi VALID ise sağlıklı", () => {
  assert.equal(healthOf(ok, null), "unknown");
  assert.equal(healthOf(ok, valid), "healthy");
});

test("bayat veri iyi haber taşımaz: healthy ve unknown → attention", () => {
  assert.equal(healthOf(ok, valid, true), "attention");
  assert.equal(healthOf(ok, null, true), "attention");
  assert.equal(healthOf(ok, valid, false), "healthy");
});

test("bayat olsa da kötü haber kalır", () => {
  assert.equal(healthOf(ok, { ...valid, daemon_status: "CHAIN_STATUS_INVALID" }, true), "tampering");
  assert.equal(healthOf({ ...ok, running_as_user: "root" }, valid, true), "broken-install");
});

test("bağlantı yoksa bayatlık anlamsız: bilinmiyor", () => {
  assert.equal(healthOf(null, null, true), "unknown");
});

test("ton ve i18n anahtarı", () => {
  assert.equal(healthTone("healthy"), "ok");
  assert.equal(healthTone("attention"), "warn");
  assert.equal(healthTone("tampering"), "bad");
  assert.equal(healthTone("broken-install"), "bad");
  assert.equal(healthTone("unknown"), "muted");
  assert.equal(healthKey("broken-install"), "health.broken_install");
  assert.equal(healthKey("healthy"), "health.healthy");
});

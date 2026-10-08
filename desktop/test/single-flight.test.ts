/**
 * Aynı sunucuya aynı anda tek istek. Üst üste binen iki istekte geç gelen
 * ESKİ cevap yenisinin üstüne yazabiliyordu: eski başarı yeni hatanın
 * "bayat" işaretini siliyor, eski VALID yeni INVALID'i gizliyordu.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { createSingleFlight } from "../src/renderer/lib/single-flight.ts";

function deferred<V>() {
  let resolve!: (v: V) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<V>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

test("aynı anahtar sürerken ikinci çağrı yeni iş başlatmaz, aynı sonucu alır", async () => {
  const flight = createSingleFlight<string>();
  const d = deferred<string>();
  let runs = 0;
  const run = () => {
    runs++;
    return d.promise;
  };
  const a = flight("srv", run);
  const b = flight("srv", run);
  d.resolve("ok");
  assert.deepEqual(await Promise.all([a, b]), ["ok", "ok"]);
  assert.equal(runs, 1);
});

test("iş bitince aynı anahtar yeniden çalışır", async () => {
  const flight = createSingleFlight<number>();
  let runs = 0;
  const run = async () => ++runs;
  assert.equal(await flight("srv", run), 1);
  assert.equal(await flight("srv", run), 2);
});

test("hata ikisine de ulaşır ve kaydı siler", async () => {
  const flight = createSingleFlight<string>();
  const d = deferred<string>();
  const a = flight("srv", () => d.promise);
  const b = flight("srv", () => Promise.resolve("ikinci iş başlamamalı"));
  d.reject(new Error("down"));
  await assert.rejects(a, /down/);
  await assert.rejects(b, /down/);
  assert.equal(await flight("srv", async () => "yeni"), "yeni");
});

test("farklı anahtarlar birbirini beklemez", async () => {
  const flight = createSingleFlight<string>();
  const slow = deferred<string>();
  const a = flight("A", () => slow.promise);
  assert.equal(await flight("B", async () => "B"), "B");
  slow.resolve("A");
  assert.equal(await a, "A");
});

/**
 * Profil yazmaları sıraya girer: ikisi üst üste binerse her biri aynı eski
 * listeden başlar, geç biten öbürünün değişikliğini geri getirir ya da siler.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { createSerial } from "../src/renderer/lib/serial.ts";

function deferred<V>() {
  let resolve!: (v: V) => void;
  let reject!: (e: unknown) => void;
  const promise = new Promise<V>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

test("ilk iş bitmeden ikinci iş başlamaz", async () => {
  const run = createSerial();
  const d = deferred<string>();
  const log: string[] = [];
  const a = run(async () => {
    log.push("a başladı");
    const v = await d.promise;
    log.push("a bitti");
    return v;
  });
  const b = run(async () => {
    log.push("b başladı");
    return "b";
  });
  await new Promise((r) => setImmediate(r));
  assert.deepEqual(log, ["a başladı"]);
  d.resolve("a");
  await Promise.all([a, b]);
  assert.deepEqual(log, ["a başladı", "a bitti", "b başladı"]);
});

test("sonuçlar doğru çağırana, çağrı sırasıyla döner", async () => {
  const run = createSerial();
  const d = deferred<number>();
  const a = run(() => d.promise);
  const b = run(async () => 2);
  const c = run(async () => 3);
  d.resolve(1);
  assert.deepEqual(await Promise.all([a, b, c]), [1, 2, 3]);
});

test("reddedilen iş yalnız kendi çağıranını reddeder, sonraki iş yine çalışır", async () => {
  const run = createSerial();
  const a = run(async () => {
    throw new Error("patladı");
  });
  const b = run(async () => "sağlam");
  await assert.rejects(a, /patladı/);
  assert.equal(await b, "sağlam");
  assert.equal(await run(async () => "sonra"), "sonra");
});

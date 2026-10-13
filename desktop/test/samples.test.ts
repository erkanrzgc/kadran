/** Çizgi grafik örnekleri: yalnız bellekte, en çok 60, en eski düşer. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { MAX_SAMPLES, pushSample, sparklinePath } from "../src/renderer/lib/samples.ts";

test("pushSample yeni dizi döndürür, eskisine dokunmaz", () => {
  const a = [1, 2];
  const b = pushSample(a, 3);
  assert.deepEqual(a, [1, 2]);
  assert.deepEqual(b, [1, 2, 3]);
});

test("en fazla MAX_SAMPLES; en eski düşer", () => {
  let s: number[] = [];
  for (let i = 0; i < MAX_SAMPLES + 5; i++) s = pushSample(s, i);
  assert.equal(s.length, MAX_SAMPLES);
  assert.equal(s[0], 5);
  assert.equal(s[s.length - 1], MAX_SAMPLES + 4);
});

test("sayı olmayan örnek eklenmez", () => {
  assert.deepEqual(pushSample([1], Number.NaN), [1]);
});

test("sparklinePath: iki noktadan azında boş; düz seride orta çizgi", () => {
  assert.equal(sparklinePath([5], 100, 20), "");
  assert.equal(sparklinePath([], 100, 20), "");
  assert.equal(sparklinePath([4, 4], 100, 20), "M0.0 10.0 L100.0 10.0");
  assert.equal(sparklinePath([0, 10], 100, 20), "M0.0 20.0 L100.0 0.0");
});

/** Takma ad başına yığın seçimi: yarış ve iptal. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { CODE_FALLBACK } from "../src/renderer/lib/apply-appearance.ts";
import { resolveStack } from "../src/renderer/lib/font-stack.ts";
import type { Face } from "../src/renderer/lib/fonts.ts";

const FACES = [{ family: "X", fullName: "X", postscriptName: "X", style: "Regular", weight: 400, italic: false }] as unknown as Face[];

test("başarılı kayıt takma adlı yığın verir", async () => {
  const s = await resolveStack("kadran-code", FACES, CODE_FALLBACK, async () => 1, () => false);
  assert.equal(s, `"kadran-code", ${CODE_FALLBACK}`);
});

test("canlı çağrıda 0 gerçek başarısızlık: yedek yığın", async () => {
  assert.equal(await resolveStack("kadran-code", FACES, CODE_FALLBACK, async () => 0, () => false), CODE_FALLBACK);
});

test("iptal edilmiş çağrı null döner, durumu ezmez", async () => {
  let cancelled = false;
  const p = resolveStack("kadran-code", FACES, CODE_FALLBACK, async () => { cancelled = true; return 0; }, () => cancelled);
  assert.equal(await p, null);
});

test("kayıt hata fırlatırsa yedek yığın", async () => {
  const boom = async (): Promise<number> => { throw new Error("FontFace"); };
  assert.equal(await resolveStack("kadran-ui", FACES, CODE_FALLBACK, boom, () => false), CODE_FALLBACK);
});

test("yüz yoksa kayıt çağrılmaz", async () => {
  let called = false;
  const s = await resolveStack("kadran-ui", [], CODE_FALLBACK, async () => { called = true; return 1; }, () => false);
  assert.equal(called, false);
  assert.equal(s, CODE_FALLBACK);
});

/**
 * Arayüzün kaynak HTML'indeki CSP'nin birebir korunduğunu doğrular.
 *
 * CSP'yi gevşetmek (örneğin 'unsafe-inline') bir XSS'i renderer'da kod
 * çalıştırmaya çevirir. Değişiklik bilinçli olacaksa bu test de bilinçli
 * değiştirilir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
export const EXPECTED_CSP =
  "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'none'; form-action 'none'; base-uri 'none'";

export function cspOf(html: string): string | null {
  const match = html.match(/http-equiv="Content-Security-Policy"\s+content="([^"]*)"/);
  return match?.[1] ?? null;
}

test("kaynak index.html CSP'yi birebir taşıyor", async () => {
  const html = await readFile(join(here, "..", "src", "renderer", "index.html"), "utf8");
  assert.equal(cspOf(html), EXPECTED_CSP);
});

test("kaynak index.html'de satır içi betik ve stil yok", async () => {
  const html = await readFile(join(here, "..", "src", "renderer", "index.html"), "utf8");
  const scripts = [...html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)];
  assert.equal(scripts.length, 1, "tek bir giriş betiği olmalı");
  assert.match(scripts[0]![1]!, /\bsrc="\.\/main\.tsx"/);
  assert.equal(scripts[0]![2]!.trim(), "", "betik gövdesi boş olmalı");
  assert.doesNotMatch(html, /<style\b/);
  assert.doesNotMatch(html, /\sstyle="/);
});

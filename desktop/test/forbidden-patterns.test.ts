/**
 * Arayüzde HTML enjekte eden hiçbir API kullanılmıyor. Sunucudan gelen
 * metin (hata, denetim alanları) güvenilmez girdi; her zaman düz metin.
 * Motion'ın <style> enjekte eden kipi de (popLayout) yasak: CSP onu
 * engeller ve sessizce bozulur.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const rendererDir = join(here, "..", "src", "renderer");
const FORBIDDEN = [/dangerouslySetInnerHTML/, /\.(?:inner|outer)HTML\s*\+?=(?!=)/, /insertAdjacentHTML/, /document\.write\(/, /popLayout/];

async function files(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const e of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, e.name);
    if (e.isDirectory()) out.push(...(await files(path)));
    else if (/\.(ts|tsx)$/.test(e.name)) out.push(path);
  }
  return out;
}

test("arayüzde HTML enjeksiyonu yok", async () => {
  const problems: string[] = [];
  const list = await files(rendererDir);
  assert.ok(list.length >= 30, `yalnız ${list.length} dosya tarandı`);
  for (const file of list) {
    const src = await readFile(file, "utf8");
    for (const re of FORBIDDEN) if (re.test(src)) problems.push(`${file}: ${re}`);
  }
  assert.deepEqual(problems, []);
});

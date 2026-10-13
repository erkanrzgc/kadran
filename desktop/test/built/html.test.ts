/**
 * Vite'ın ürettiği out/renderer/index.html'i ölçer: CSP aynı mı, satır içi
 * betik ya da stil var mı, yollar göreli mi, başvurulan dosyalar var mı.
 * Vite'ın varsayılan çıktısına güvenilmiyor; ölçülüyor.
 *
 * Önce `npm run build` çalışmış olmalı.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { existsSync } from "node:fs";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { cspOf, EXPECTED_CSP } from "../csp.test.ts";

const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", "..", "out", "renderer");

test("derlenmiş index.html CSP'yi koruyor ve satır içi kod taşımıyor", async () => {
  const html = await readFile(join(outDir, "index.html"), "utf8");
  assert.equal(cspOf(html), EXPECTED_CSP);

  for (const m of html.matchAll(/<script\b([^>]*)>([\s\S]*?)<\/script>/g)) {
    assert.match(m[1]!, /\bsrc="/, "src'siz betik var");
    assert.equal(m[2]!.trim(), "", "betik gövdesi boş değil");
  }
  assert.doesNotMatch(html, /<style\b/);
  assert.doesNotMatch(html, /\sstyle="/);
});

test("derlenmiş index.html'deki yollar göreli ve dosyalar mevcut", async () => {
  const html = await readFile(join(outDir, "index.html"), "utf8");
  const refs = [...html.matchAll(/\b(?:src|href)="([^"]+)"/g)].map((m) => m[1]!);
  assert.ok(refs.length >= 2, `en az bir JS ve bir CSS bekleniyordu: ${refs.join(", ")}`);
  for (const ref of refs) {
    assert.match(ref, /^\.\//, `göreli olmayan yol: ${ref}`);
    assert.ok(existsSync(join(outDir, ref)), `dosya yok: ${ref}`);
  }
});

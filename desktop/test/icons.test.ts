/**
 * Kopyalanan ikonlar: lisans başlığı var, shadcn'in "@/lib/utils" içe
 * aktarması kalmamış, "use client" kalmamış, motion/react kullanıyor;
 * LICENSE iki lisansı da (MIT + ISC) taşıyor.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const iconsDir = join(here, "..", "src", "renderer", "icons");
const EXPECTED = ["activity", "badge-alert", "check", "chevron-down", "copy", "history", "refresh-cw", "server", "settings", "shield-check"];

test("beklenen ikonlar kopyalanmış", async () => {
  const files = (await readdir(iconsDir)).filter((f) => f.endsWith(".tsx")).map((f) => f.slice(0, -4)).sort();
  assert.deepEqual(files, EXPECTED);
});

test("her ikon temizlenmiş ve lisans başlığı taşıyor", async () => {
  for (const name of EXPECTED) {
    const src = await readFile(join(iconsDir, `${name}.tsx`), "utf8");
    assert.match(src, /pqoqubbw\/icons@072c38b1b04ea738d90a084485ccaad4b890ddca/, name);
    assert.match(src, /MIT/, name);
    assert.doesNotMatch(src, /@\/lib\/utils/, name);
    assert.doesNotMatch(src, /"use client"/, name);
    assert.doesNotMatch(src, /\bReact\.MouseEvent/, name);
    assert.match(src, /from "motion\/react"/, name);
  }
});

test("LICENSE MIT ve ISC metinlerini taşıyor", async () => {
  const lic = await readFile(join(iconsDir, "LICENSE"), "utf8");
  assert.match(lic, /MIT License/);
  assert.match(lic, /pqoqubbw/);
  assert.match(lic, /ISC License/);
  assert.match(lic, /Lucide/);
});

test("index.ts dışa aktarım adları sözleşmeyi karşılıyor", async () => {
  const indexSrc = await readFile(join(iconsDir, "index.ts"), "utf8");
  const lines = indexSrc.trim().split("\n");
  const exported = [];
  for (const line of lines) {
    const m = line.match(/export \{ (\w+)(?: as (\w+))? \}/);
    if (m) {
      exported.push(m[2] || m[1]);
    }
  }
  const expected = [
    "ActivityIcon",
    "BadgeAlertIcon",
    "CheckIcon",
    "ChevronDownIcon",
    "CopyIcon",
    "HistoryIcon",
    "RefreshCwIcon",
    "ServerIcon",
    "SettingsIcon",
    "ShieldCheckIcon",
  ];
  assert.deepEqual(exported.sort(), expected);
});

test("hiçbir ikon Tailwind overflow-visible sınıfını içermiyor", async () => {
  for (const name of EXPECTED) {
    const src = await readFile(join(iconsDir, `${name}.tsx`), "utf8");
    assert.doesNotMatch(src, /className="overflow-visible"/, name);
  }
});

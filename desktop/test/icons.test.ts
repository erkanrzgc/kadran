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

// Bütünlük doğrulama modülü testleri
import { verifySha256, validateKeySet } from "../scripts/vendor-integrity.mjs";

test("verifySha256: uyuşan özet geçer", () => {
  const testMap = { "test": "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" };
  // "hello" dizesi için SHA-256
  assert.doesNotThrow(() => verifySha256("hello", "test", testMap));
});

test("verifySha256: uyuşmayan özet hatası verir", () => {
  const testMap = { "test": "0000000000000000000000000000000000000000000000000000000000000000" };
  assert.throws(
    () => verifySha256("hello", "test", testMap),
    /Bütünlük hatası: test/
  );
});

test("verifySha256: eksik anahtar hatası verir", () => {
  const testMap = {};
  assert.throws(
    () => verifySha256("hello", "missing", testMap),
    /Eksik özet anahtarı: missing/
  );
});

test("validateKeySet: uyuşan anahtar seti geçer", () => {
  const names = ["a", "b"];
  const expectedMap = {
    a: "hash1",
    b: "hash2",
    "LICENSE:pqoqubbw": "hash3",
    "LICENSE:lucide": "hash4",
  };
  assert.doesNotThrow(() => validateKeySet(names, expectedMap));
});

test("validateKeySet: eksik anahtar hatası verir", () => {
  const names = ["a", "b"];
  const expectedMap = {
    a: "hash1",
    b: "hash2",
    // LICENSE:pqoqubbw eksik
    "LICENSE:lucide": "hash4",
  };
  assert.throws(
    () => validateKeySet(names, expectedMap),
    /Eksik: LICENSE:pqoqubbw/
  );
});

test("validateKeySet: fazla anahtar hatası verir", () => {
  const names = ["a"];
  const expectedMap = {
    a: "hash1",
    b: "hash2", // fazla
    "LICENSE:pqoqubbw": "hash3",
    "LICENSE:lucide": "hash4",
  };
  assert.throws(
    () => validateKeySet(names, expectedMap),
    /Fazla: b/
  );
});

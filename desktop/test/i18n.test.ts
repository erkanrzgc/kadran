/**
 * Dil dosyaları: iki dilin anahtarları birebir aynı, yer tutucuları aynı,
 * İngilizce dosyada Türkçe harf yok, koddaki her t("…") anahtarı var.
 * Yeni bir ekrana Türkçesi eklenmezse CI burada kırmızı yanar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { placeholders, translate } from "../src/renderer/lib/i18n.ts";

const here = dirname(fileURLToPath(import.meta.url));
const i18nDir = join(here, "..", "src", "renderer", "i18n");
const rendererDir = join(here, "..", "src", "renderer");

async function dict(lang: string): Promise<Record<string, string>> {
  return JSON.parse(await readFile(join(i18nDir, `${lang}.json`), "utf8")) as Record<string, string>;
}

async function sourceFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "icons") out.push(...(await sourceFiles(path)));
    } else if (/\.(ts|tsx)$/.test(entry.name)) {
      out.push(path);
    }
  }
  return out;
}

test("en ve tr anahtarları birebir aynı", async () => {
  const en = Object.keys(await dict("en")).sort();
  const tr = Object.keys(await dict("tr")).sort();
  assert.deepEqual(tr, en);
});

test("her anahtarın yer tutucuları iki dilde aynı", async () => {
  const en = await dict("en");
  const tr = await dict("tr");
  for (const [key, value] of Object.entries(en)) {
    assert.deepEqual(placeholders(tr[key] ?? ""), placeholders(value), `yer tutucu farkı: ${key}`);
  }
});

test("İngilizce dosyada Türkçe harf yok", async () => {
  for (const [key, value] of Object.entries(await dict("en"))) {
    assert.doesNotMatch(value, /[çğıöşüÇĞİÖŞÜ]/, `Türkçe metin: ${key} = ${value}`);
  }
});

/**
 * Türkçe harf denetimi "Ayarlar" gibi harfsiz bir Türkçe kelimeyi
 * kaçırıyordu (mutate-desktop.sh'ta yakalanmayan mutant). İki dilde aynı
 * değer yalnız dilden bağımsız anahtarlarda olabilir: marka, teknik terim,
 * örnek hedef. Böylece İngilizceye sızan Türkçe de, çevrilmeden kalan
 * Türkçe de düşer.
 */
const SAME_IN_BOTH = new Set([
  "rail.target_placeholder",
  "rail.version",
  "topbar.rtt",
  "tile.executor",
  "card.daemon",
  "card.executor",
  "row.docker",
  "mode.mica",
  "mode.vibrancy",
]);

test("iki dilde aynı değer yalnız dilden bağımsız anahtarlarda", async () => {
  const en = await dict("en");
  const tr = await dict("tr");
  const same = Object.keys(en).filter((k) => en[k] === tr[k]);
  assert.deepEqual(
    same.filter((k) => !SAME_IN_BOTH.has(k)),
    [],
    "çevrilmemiş ya da öbür dile sızmış metin",
  );
  // Liste dürüst kalsın: artık farklı olan anahtar listeden çıkarılmalı.
  assert.deepEqual(
    [...SAME_IN_BOTH].filter((k) => en[k] !== tr[k]),
    [],
    "SAME_IN_BOTH'ta artık iki dilde farklı olan anahtar var",
  );
});

test("koddaki her t(\"…\") anahtarı en.json'da var", async () => {
  const en = await dict("en");
  const missing: string[] = [];
  for (const file of await sourceFiles(rendererDir)) {
    const src = await readFile(file, "utf8");
    for (const m of src.matchAll(/\bt\(\s*"([a-z0-9_.]+)"/g)) {
      const key = m[1]!;
      if (!(key in en) && !(`${key}_one` in en && `${key}_other` in en)) missing.push(`${file}: ${key}`);
    }
  }
  assert.deepEqual(missing, []);
});

test("translate: yer tutucu, çoğul, yedek dil, bilinmeyen anahtar", () => {
  const en = { hello: "Hi {name}", "n_one": "1 thing", "n_other": "{count} things", only_en: "fallback" };
  const tr = { hello: "Selam {name}", "n_one": "1 şey", "n_other": "{count} şey" };
  assert.equal(translate(tr, en, "hello", { name: "Ali" }), "Selam Ali");
  assert.equal(translate(tr, en, "n", { count: 1 }), "1 şey");
  assert.equal(translate(tr, en, "n", { count: 3 }), "3 şey");
  assert.equal(translate(tr, en, "only_en"), "fallback");
  assert.equal(translate(tr, en, "yok.boyle"), "yok.boyle");
  assert.equal(translate(tr, en, "hello"), "Selam {name}");
});

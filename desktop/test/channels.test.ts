/**
 * Preload'daki kanal adlarının paylaşılan listeyle aynı olduğunu doğrular.
 *
 * # Neden bir kopya var ve neden test ediliyor?
 *
 * Preload betiği CommonJS olmak zorunda (sandbox açıkken Electron ESM
 * preload çalıştırmıyor), ana süreç ise ESM. İkisi aynı modülü içe
 * aktaramıyor, bu yüzden kanal adları preload tarafında elle yazılı.
 *
 * Kopya listeler sessizce ayrışır: bir kanal yeniden adlandırılır, diğer
 * taraf güncellenmez ve çağrı çalışma anında "no handler registered"
 * hatasıyla ölür — hem de yalnızca o özelliği kullanan kullanıcıda.
 * Derleyici bunu yakalayamaz; test yakalar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { CHANNELS } from "../src/shared/channels.ts";

const here = dirname(fileURLToPath(import.meta.url));
const preloadPath = join(here, "..", "src", "preload", "index.cjs");

/** preloadChannels, .cjs dosyasındaki CHANNELS nesnesini ayıklar. */
async function preloadChannels(): Promise<Record<string, string>> {
  const source = await readFile(preloadPath, "utf8");

  const block = source.match(/const CHANNELS = \{([\s\S]*?)\};/);
  assert.ok(block, "preload içinde CHANNELS nesnesi bulunamadı");

  const entries: Record<string, string> = {};
  for (const line of block[1].split("\n")) {
    const match = line.match(/^\s*(\w+)\s*:\s*"([^"]+)"\s*,?\s*$/);
    if (match) entries[match[1]] = match[2];
  }
  return entries;
}

test("preload ve paylaşılan kanal listeleri birebir aynı", async () => {
  const fromPreload = await preloadChannels();

  assert.deepEqual(
    fromPreload,
    { ...CHANNELS },
    "kanal listeleri ayrıştı — biri güncellenmiş, diğeri unutulmuş olabilir",
  );
});

test("her kanal adı panely: önekini taşıyor", () => {
  for (const [key, value] of Object.entries(CHANNELS)) {
    assert.match(value, /^panely:/, `${key} kanalı öneki taşımıyor: ${value}`);
  }
});

test("kanal adları benzersiz", () => {
  const values = Object.values(CHANNELS);
  assert.equal(
    new Set(values).size,
    values.length,
    "iki kanal aynı adı taşıyor — biri diğerinin işleyicisini gölgeler",
  );
});

/**
 * Preload'un yüzeyi kasten dar tutuluyor. Bu test, oraya genel amaçlı bir
 * köprünün (örneğin `invoke(kanal, ...)`) sızmasını yakalar: öyle bir
 * köprü, sunucudaki tipli RPC sınırını istemci tarafından delerdi.
 */
test("preload genel amaçlı bir köprü açmıyor", async () => {
  const source = await readFile(preloadPath, "utf8");

  const exposed = source.match(/exposeInMainWorld\(\s*"panely"\s*,\s*\{([\s\S]*)\}\s*\)/);
  assert.ok(exposed, "exposeInMainWorld çağrısı bulunamadı");

  const body = exposed[1];
  // ipcRenderer.invoke her zaman SABİT bir kanalla çağrılmalı.
  // `invoke(channel` gibi değişken bir kanal, beyaz listeyi anlamsız kılar.
  const invocations = [...body.matchAll(/ipcRenderer\.invoke\(\s*([^,)]+)/g)];
  assert.ok(invocations.length > 0, "hiç invoke çağrısı bulunamadı");

  for (const [, arg] of invocations) {
    assert.match(
      arg.trim(),
      /^CHANNELS\.\w+$/,
      `invoke sabit olmayan bir kanalla çağrılıyor: ${arg.trim()}`,
    );
  }
});

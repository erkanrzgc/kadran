/**
 * Tema geçişi: cam ile arayüz aynı anda, ekran tek seferde döner (K-163).
 * Cam tonu tema başına; kayıt beklerse arayüz açıkken arkada koyu cam kalırdı.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { saveDelay, SAVE_DELAY_MS, switchTheme, SWITCHING_CLASS, type ThemeRoot } from "../src/renderer/lib/theme-switch.ts";
import { defaultAppearance } from "../src/shared/appearance.ts";

const A = defaultAppearance();

test("tema değişince kayıt beklemez; yazı tipi ve dil kısa bekler", () => {
  assert.equal(saveDelay(A, { ...A, theme: "dark" }), 0);
  assert.equal(saveDelay({ ...A, theme: "dark" }, A), 0);
  assert.equal(saveDelay(A, { ...A, uiFont: { family: "Arial", weight: 500 } }), SAVE_DELAY_MS);
  assert.equal(saveDelay(A, { ...A, language: "tr" }), SAVE_DELAY_MS);
  assert.ok(SAVE_DELAY_MS > 0);
});

test("geçişte animasyonlar kapanır, renkler tek seferde döner, sonra animasyonlar geri gelir", () => {
  const log: string[] = [];
  let pending: (() => void) | null = null;
  const dataset: Record<string, string | undefined> = { theme: "light" };
  const root: ThemeRoot = {
    dataset,
    classList: { add: (c) => log.push(`+${c}`), remove: (c) => log.push(`-${c}`) },
  };
  switchTheme(root, "dark", () => log.push(`settle theme=${dataset.theme}`), (fn) => {
    pending = fn;
  });
  // Sınıf temadan ÖNCE eklenir, renkler animasyonsuz hesaplanır; sınıf ancak sonra kalkar.
  assert.deepEqual(log, [`+${SWITCHING_CLASS}`, "settle theme=dark"]);
  assert.equal(dataset.theme, "dark");
  assert.ok(pending, "animasyonlar geri açılmak üzere sıraya konmalı");
  (pending as () => void)();
  assert.deepEqual(log, [`+${SWITCHING_CLASS}`, "settle theme=dark", `-${SWITCHING_CLASS}`]);
});

test("ilk çizimde ve aynı temada geçiş yok", () => {
  const log: string[] = [];
  const first: Record<string, string | undefined> = {};
  const classList = { add: (c: string) => log.push(`+${c}`), remove: (c: string) => log.push(`-${c}`) };
  switchTheme({ dataset: first, classList }, "light", () => log.push("settle"), () => log.push("later"));
  assert.equal(first.theme, "light");
  const same: Record<string, string | undefined> = { theme: "dark" };
  switchTheme({ dataset: same, classList }, "dark", () => log.push("settle"), () => log.push("later"));
  assert.equal(same.theme, "dark");
  assert.deepEqual(log, []);
});

test("ana süreç camı diske yazmadan önce değiştirir: kayıt beklerse cam arayüzden geç döner", async () => {
  const { readFile } = await import("node:fs/promises");
  const index = await readFile(new URL("../src/main/index.ts", import.meta.url), "utf8");
  const handler = index.slice(index.indexOf("CHANNELS.saveAppearance"));
  const look = handler.indexOf("host?.setLook(");
  const write = handler.indexOf("saveAppearance(userData()");
  assert.ok(look >= 0 && write >= 0);
  assert.ok(look < write, "cam diske yazmadan önce");
});

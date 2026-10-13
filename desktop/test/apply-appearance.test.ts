/** Görünüm → CSS değişkenleri ve kök öznitelikleri. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { ACCENT_RGB, applyAppearance, appearanceVars, CODE_FALLBACK, fontStack, UI_FALLBACK } from "../src/renderer/lib/apply-appearance.ts";
import { defaultAppearance } from "../src/shared/appearance.ts";

const A = { ...defaultAppearance({ platform: "win32", release: "10.0.26200" }), tint: 48, panel: 6, edge: 8, glow: 75, accent: "amber" as const, tone: "light" as const };
const STACKS = { ui: fontStack("kadran-ui", UI_FALLBACK), code: CODE_FALLBACK, strong: 700 };

test("kaydırıcılar 0–1 aralığına, vurgu RGB üçlüsüne", () => {
  const v = appearanceVars(A, STACKS);
  assert.equal(v["--tint"], "0.48");
  assert.equal(v["--panel"], "0.06");
  assert.equal(v["--edge"], "0.08");
  assert.equal(v["--glow"], "0.75");
  assert.equal(v["--acc"], ACCENT_RGB.amber);
  assert.equal(v["--w-strong"], "700");
  assert.equal(v["--font-ui"], `"kadran-ui", ${UI_FALLBACK}`);
  assert.equal(v["--font-mono"], CODE_FALLBACK);
});

test("fontStack: takma ad yoksa yedek yığın", () => {
  assert.equal(fontStack(null, UI_FALLBACK), UI_FALLBACK);
  assert.match(CODE_FALLBACK, /^"JetBrains Mono Bundled"/);
});

test("applyAppearance değişkenleri ve öznitelikleri yazar", () => {
  const props = new Map<string, string>();
  const target = { style: { setProperty: (k: string, v: string) => props.set(k, v) }, dataset: {} as Record<string, string | undefined> };
  applyAppearance(target, A, STACKS);
  assert.equal(props.get("--tint"), "0.48");
  assert.equal(target.dataset.mode, "acrylic");
  assert.equal(target.dataset.tone, "light");
});

test("güvenlik: kullanıcının yazı tipi adı CSS'e ASLA ulaşmaz", () => {
  const hostile = 'x", red; --evil: "';
  const evil = { ...A, uiFont: { ...A.uiFont, family: hostile }, codeFont: { ...A.codeFont, family: hostile } };
  const props = new Map<string, string>();
  const target = { style: { setProperty: (k: string, v: string) => props.set(k, v) }, dataset: {} as Record<string, string | undefined> };
  // Sağlayıcının kurduğu gibi: yığınlar yalnız sabit takma adlardan.
  const stacks = { ui: fontStack("kadran-ui", UI_FALLBACK), code: fontStack("kadran-code", CODE_FALLBACK), strong: 600 };
  applyAppearance(target, evil, stacks);
  const all = [...Object.entries(appearanceVars(evil, stacks)).flat(), ...props.keys(), ...props.values(), ...Object.values(target.dataset)].join("\n");
  assert.ok(!all.includes(hostile));
  assert.ok(!all.includes("--evil"));
  assert.ok(!all.includes("red;"));
});

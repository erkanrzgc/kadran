/** Ayarlar → CSS değişkenleri. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { applyAppearance, appearanceVars, CODE_FALLBACK, fontStack, UI_FALLBACK } from "../src/renderer/lib/apply-appearance.ts";
import { defaultAppearance } from "../src/shared/appearance.ts";

const A = defaultAppearance();
const STACKS = { ui: fontStack("kadran-ui", UI_FALLBACK), code: CODE_FALLBACK, strong: 700 };

test("yazı tipi değişkenleri", () => {
  const v = appearanceVars(A, STACKS);
  assert.equal(v["--w-strong"], "700");
  assert.equal(v["--font-ui"], `"kadran-ui", ${UI_FALLBACK}`);
  assert.equal(v["--font-mono"], CODE_FALLBACK);
});

test("fontStack: takma ad yoksa gömülü Geist", () => {
  assert.equal(fontStack(null, UI_FALLBACK), UI_FALLBACK);
  assert.match(UI_FALLBACK, /^"Geist Bundled"/);
  assert.match(CODE_FALLBACK, /^"Geist Mono Bundled"/);
});

test("applyAppearance değişkenleri yazar", () => {
  const props = new Map<string, string>();
  applyAppearance({ style: { setProperty: (k: string, v: string) => props.set(k, v) } }, A, STACKS);
  assert.equal(props.get("--w-strong"), "700");
});

test("güvenlik: kullanıcının yazı tipi adı CSS'e ASLA ulaşmaz", () => {
  const hostile = 'x", red; --evil: "';
  const evil = { ...A, uiFont: { ...A.uiFont, family: hostile }, codeFont: { ...A.codeFont, family: hostile } };
  const props = new Map<string, string>();
  // Sağlayıcının kurduğu gibi: yığınlar yalnız sabit takma adlardan.
  const stacks = { ui: fontStack("kadran-ui", UI_FALLBACK), code: fontStack("kadran-code", CODE_FALLBACK), strong: 600 };
  applyAppearance({ style: { setProperty: (k: string, v: string) => props.set(k, v) } }, evil, stacks);
  const all = [...Object.entries(appearanceVars(evil, stacks)).flat(), ...props.keys(), ...props.values()].join("\n");
  assert.ok(!all.includes(hostile));
  assert.ok(!all.includes("--evil"));
  assert.ok(!all.includes("red;"));
});

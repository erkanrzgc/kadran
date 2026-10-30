/**
 * Onaylı görsel dilin ölçülebilir kuralları (docs/specs/2026-10-30-filo-design.md).
 * Stil dosyaları metin olarak okunur: görünümün tamamını kanıtlamaz, ama
 * "köşeler yine yuvarlandı" gibi geri kaymaları yakalar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const stylesDir = join(here, "..", "src", "renderer", "styles");
const rendererDir = join(here, "..", "src", "renderer");

async function styles(): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  for (const name of await readdir(stylesDir)) out.set(name, await readFile(join(stylesDir, name), "utf8"));
  return out;
}
const read = async (name: string): Promise<string> => (await styles()).get(name) ?? "";

test("köşeler yalnız belirteçten gelir; yuvarlak yalnız hap ve nokta", async () => {
  const bad: string[] = [];
  for (const [name, css] of await styles()) {
    if (name === "tokens.css") continue;
    for (const match of css.matchAll(/border-radius:\s*([^;}]+)/g)) {
      const value = match[1]!.trim();
      if (!/^(var\(--radius(-sm|-xs)?\)|50%|999px|0|inherit)$/.test(value)) bad.push(`${name}: ${value}`);
    }
  }
  assert.deepEqual(bad, []);
});

test("köşe belirteçleri spec ile aynı", async () => {
  const tokens = await read("tokens.css");
  assert.match(tokens, /--radius:\s*6px/);
  assert.match(tokens, /--radius-sm:\s*4px/);
  assert.match(tokens, /--radius-xs:\s*3px/);
});

test("cam gerçek bulanıklık taşır, küçük öğeler cam değil", async () => {
  const glass = await read("glass.css");
  assert.match(glass, /backdrop-filter:\s*blur\(30px\) saturate\(165%\) brightness\(1\.06\)/);
  const controls = await read("controls.css");
  assert.doesNotMatch(controls, /backdrop-filter/);
});

test("küçük öğelerde sabit beyaz yok: açık temada kaybolurdu", async () => {
  const controls = await read("controls.css");
  for (const selector of [".field {", ".sel {", ".btn-ghost {", ".seg button {", ".nav-item {"]) {
    const css = (await styles()).get(selector === ".nav-item {" ? "layout.css" : "controls.css") ?? controls;
    const start = css.indexOf(selector);
    assert.ok(start >= 0, `${selector} bulunamadı`);
    const block = css.slice(start, css.indexOf("}", start));
    assert.doesNotMatch(block, /rgba\(255,\s*255,\s*255/, selector);
  }
});

test("işletim sisteminin 'saydamlığı azalt' ayarına uyulur", async () => {
  assert.match(await read("glass.css"), /@media \(prefers-reduced-transparency: reduce\)/);
});

test("yerel malzemede pencere tonu tabanı var: beyaz duvar kâğıdında yazı okunur kalsın", async () => {
  const base = await read("base.css");
  assert.match(base, /\[data-mode="acrylic"\][^{]*\{[^}]*max\(var\(--tint\), 0\.55\)/);
});

test("sürükleme bölgesi düğmeleri ve açılan menüyü yutmaz", async () => {
  const layout = await read("layout.css");
  assert.match(layout, /-webkit-app-region:\s*drag/);
  const noDrag = layout.slice(layout.indexOf("/* no-drag"));
  for (const selector of ["button", ".crumb-menu", ".operation-dialog"]) assert.ok(noDrag.includes(selector), selector);
});

/** Parçanın rengi, alfa ile arkaya bindirilmiş hâli. */
const over = (fg: readonly number[], alpha: number, bg: readonly number[]): number[] => fg.map((v, i) => v * alpha + bg[i]! * (1 - alpha));
const luminance = (rgb: readonly number[]): number =>
  rgb.map((v) => v / 255).map((v) => (v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4)).reduce((sum, v, i) => sum + v * [0.2126, 0.7152, 0.0722][i]!, 0);
const contrast = (a: readonly number[], b: readonly number[]): number => {
  const [x, y] = [luminance(a), luminance(b)];
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
};

function themeTokens(css: string, selector: string): { alphaOf: (name: string) => number; rgbOf: (name: string) => number[] } {
  const start = css.indexOf(selector);
  const block = css.slice(start, css.indexOf("}", start));
  const value = (name: string): string => {
    const match = new RegExp(`${name}:\\s*([^;]+);`).exec(block);
    assert.ok(match, `${selector} ${name} yok`);
    return match![1]!;
  };
  return {
    alphaOf: (name) => Number(/rgba\([^)]*,\s*([\d.]+)\)/.exec(value(name))?.[1] ?? 1),
    rgbOf: (name) => {
      const raw = value(name);
      const hex = /#([0-9a-f]{6})/i.exec(raw)?.[1];
      if (hex) return [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16));
      return raw.replace(/rgba?\(/, "").split(",").slice(0, 3).map((v) => Number(v.trim()));
    },
  };
}

test("en kötü duvar kâğıdında kenar çubuğu metni okunur (yerel malzeme tabanı .55)", async () => {
  const css = await read("tokens.css");
  for (const [selector, walls] of [[":root {", [[255, 255, 255]]], [':root[data-tone="light"] {', [[0, 0, 0]]]] as const) {
    const theme = themeTokens(css, selector);
    const dark = themeTokens(css, ":root {");
    const pick = (name: string): number[] => {
      try { return theme.rgbOf(name); } catch { return dark.rgbOf(name); }
    };
    for (const wall of walls) {
      const shell = over(pick("--tone"), 0.55, wall);
      const surface = over(pick("--surface"), 0.46, shell);
      const fg = pick("--fg");
      const fgOn = (name: string): number[] => over(theme.rgbOf(name), theme.alphaOf(name), surface);
      assert.ok(contrast(fg, surface) >= 4.5, `${selector} --fg ${contrast(fg, surface).toFixed(2)}`);
      assert.ok(contrast(fgOn("--fg-dim"), surface) >= 4.5, `${selector} --fg-dim ${contrast(fgOn("--fg-dim"), surface).toFixed(2)}`);
      assert.ok(contrast(fgOn("--fg-faint"), surface) >= 3, `${selector} --fg-faint ${contrast(fgOn("--fg-faint"), surface).toFixed(2)}`);
    }
  }
});

test("metin karakteri simge yok", async () => {
  const apps = await readFile(join(rendererDir, "screens", "AppsScreen.tsx"), "utf8");
  assert.doesNotMatch(apps, /◇/);
});

test("logo gömülü dış hat değil metin: Segoe UI yeniden dağıtılamaz", async () => {
  const brand = await readFile(join(rendererDir, "components", "BrandWordmark.tsx"), "utf8");
  assert.match(brand, /KΛDRΛN/);
  assert.doesNotMatch(brand, /<path/);
  const layout = await read("layout.css");
  assert.match(layout, /\.marka \{[^}]*letter-spacing:\s*0\.22em/);
  assert.match(layout, /\.marka \{[^}]*font-weight:\s*700/);
});

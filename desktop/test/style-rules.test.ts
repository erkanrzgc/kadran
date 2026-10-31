/**
 * Görsel dilin ölçülebilir kuralları (docs/decisions.md K-161). Stil dosyaları
 * metin olarak okunur: görünümün tamamını kanıtlamaz, ama geri kaymaları yakalar.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const rendererDir = join(here, "..", "src", "renderer");
const stylesDir = join(rendererDir, "styles");

async function styles(): Promise<Map<string, string>> {
  const out = new Map<string, string>();
  for (const name of await readdir(stylesDir)) out.set(name, await readFile(join(stylesDir, name), "utf8"));
  return out;
}
const read = async (name: string): Promise<string> => (await styles()).get(name) ?? "";

test("Coolify ölçüleri: kart 12, denetim 8, küçük 6 px", async () => {
  const tokens = await read("tokens.css");
  assert.match(tokens, /--radius-card:\s*12px/);
  assert.match(tokens, /--radius:\s*8px/);
  assert.match(tokens, /--radius-sm:\s*6px/);
  const layout = await read("layout.css");
  assert.match(layout, /\.rail \{ width: 224px/);
  assert.match(layout, /\.topbar \{ height: 48px/);
});

test("kenar çubuğunda dolgu ve mor yok; seçili satır cam hap", async () => {
  const layout = await read("layout.css");
  const rail = layout.slice(layout.indexOf(".rail {"), layout.indexOf("}", layout.indexOf(".rail {")));
  assert.doesNotMatch(rail, /background/);
  assert.match(layout, /\.nav-item\.is-active \{[^}]*linear-gradient\(180deg, rgba\(255, 255, 255/);
  assert.doesNotMatch(layout, /7317ff|5a12c7|6b16ed|120e1a/);
});

test("mor yalnız ana düğmede", async () => {
  for (const [name, css] of await styles()) {
    if (name === "tokens.css") continue;
    for (const hit of css.matchAll(/var\(--accent-[a-z]+\)/g)) {
      const before = css.slice(0, hit.index);
      assert.ok(before.lastIndexOf(".btn-primary") > before.lastIndexOf("}"), `${name}: ana düğme dışında mor`);
    }
  }
});

test("pencere modu ve cam ayarı kalmadı", async () => {
  for (const [name, css] of await styles()) {
    assert.doesNotMatch(css, /data-mode|data-tone|--panel|--glow/, name);
  }
  const tokens = await read("tokens.css");
  assert.match(tokens, /--tint:\s*0\.34/);
});

test("yerel malzeme yoksa ya da saydamlık kapalıysa opak", async () => {
  const base = await read("base.css");
  assert.match(base, /\[data-material="none"\] \.shell \{ background: #16161a/);
  assert.match(base, /@media \(prefers-reduced-transparency: reduce\)/);
});

test("yüzen yüzey opak: menü, kutu, bildirim", async () => {
  const tokens = await read("tokens.css");
  assert.match(tokens, /--menu:\s*rgb\(/);
  assert.match(await read("layout.css"), /\.crumb-menu \{[^}]*background: var\(--menu\)/);
  assert.match(await read("management.css"), /\.operation-dialog \{[^}]*background: var\(--menu\)/);
  assert.match(await read("controls.css"), /\.toast \{[^}]*background: var\(--menu\)/);
});

test("sürükleme bölgesi düğmeleri ve açılan menüyü yutmaz", async () => {
  const layout = await read("layout.css");
  assert.match(layout, /\.rail-brand, \.topbar \{ -webkit-app-region: drag; \}/);
  assert.match(layout, /button, input, select, a, summary, \.crumb-menu, \.operation-dialog, \.toast \{ -webkit-app-region: no-drag; \}/);
});

test("Geist iki alt kümeyle gömülü: ş, ğ, İ dosyada", async () => {
  const fonts = await read("fonts.css");
  assert.match(fonts, /"Geist Bundled"/);
  assert.match(fonts, /"Geist Mono Bundled"/);
  assert.match(fonts, /geist-latin-ext-wght-normal\.woff2/);
  assert.match(fonts, /U\+0100-02BA/);
  const files = await readdir(join(rendererDir, "fonts"));
  for (const f of ["geist-latin-wght-normal.woff2", "geist-latin-ext-wght-normal.woff2", "geist-mono-latin-wght-normal.woff2", "geist-mono-latin-ext-wght-normal.woff2"]) assert.ok(files.includes(f), f);
});

test("logo gömülü dış hat değil metin", async () => {
  const brand = await readFile(join(rendererDir, "components", "BrandWordmark.tsx"), "utf8");
  assert.match(brand, /KΛDRΛN/);
  assert.doesNotMatch(brand, /<path/);
  assert.match(await read("layout.css"), /\.marka \{[^}]*font-weight: 700; letter-spacing: 0\.22em/);
});

test("metin karakteri simge yok", async () => {
  const apps = await readFile(join(rendererDir, "screens", "AppsScreen.tsx"), "utf8");
  assert.doesNotMatch(apps, /◇/);
});

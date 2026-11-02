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
});

test("üst çubuk düz: zemin, gölge, yüzme yok; düğmeler sağ köşede", async () => {
  const layout = await read("layout.css");
  const bar = layout.slice(layout.indexOf(".topbar {"), layout.indexOf("}", layout.indexOf(".topbar {")));
  assert.match(bar, /height: 48px/);
  assert.doesNotMatch(bar, /position: absolute|background|box-shadow|border-radius|filter/);
  assert.match(layout, /\.rail-brand \{ height: 48px;/);
  assert.match(layout, /\.win-btns \{[^}]*margin-left: auto/);
  assert.doesNotMatch(layout, /\.lens|--bar-/);
  assert.doesNotMatch(await read("tokens.css"), /--bar-/);
});

test("kenar çubuğunda dolgu ve mor yok; seçili satır köşeli cam", async () => {
  const layout = await read("layout.css");
  const rail = layout.slice(layout.indexOf(".rail {"), layout.indexOf("}", layout.indexOf(".rail {")));
  assert.doesNotMatch(rail, /background/);
  assert.match(layout, /\.nav-item \{[^}]*border-radius: var\(--radius\);/);
  assert.match(layout, /\.nav-item::before \{[^}]*background: var\(--pill\)[^}]*opacity: 0/);
  assert.match(layout, /\.nav-item\.is-active::before \{ opacity: 1;/);
  // Hover seçili satırı ezerse fare üstündeyken cam kaybolur, çekilince birden gelir.
  assert.match(layout, /\.nav-item:not\(:disabled\):not\(\.is-active\):hover \{/);
  assert.match(await read("tokens.css"), /--pill: linear-gradient\(180deg, rgba\(255, 255, 255/);
  assert.doesNotMatch(layout, /7317ff|5a12c7|6b16ed|120e1a/);
});

test("üç tema da aynı renk adlarını tanımlıyor; bileşende sabit beyaz ya da siyah yok", async () => {
  const tokens = await read("tokens.css");
  const block = (selector: string): string => {
    const start = tokens.indexOf(`${selector} {`);
    assert.ok(start >= 0, selector);
    return tokens.slice(start, tokens.indexOf("}", start));
  };
  const named = (css: string): string[] => [...css.matchAll(/(--[a-z-]+):/g)].map((m) => m[1]!);
  const glass = named(block(":root"));
  for (const selector of [':root[data-theme="dark"]', ':root[data-theme="light"]']) {
    const own = named(block(selector));
    for (const name of ["--shell", "--ov", "--fg", "--fg-dim", "--fg-faint", "--line", "--edge", "--flat", "--menu", "--card-bg", "--well-bg", "--pill", "--text-shadow", "--kart-bg", "--kart-shadow"]) {
      assert.ok(glass.includes(name), `cam: ${name}`);
      assert.ok(own.includes(name), `${selector}: ${name}`);
    }
  }
  assert.match(block(':root[data-theme="light"]'), /color-scheme: light/);
  for (const [name, css] of await styles()) {
    if (name === "tokens.css") continue;
    assert.doesNotMatch(css, /rgba\(255, 255, 255|rgba\(0, 0, 0|#1b1b1f|#16161a|#f2f2f2/, name);
  }
});

test("odak gidince siyah yalnız yedek Acrylic'te; Windows camı açık kalır", async () => {
  const tokens = await read("tokens.css");
  assert.match(tokens, /:root\[data-theme="glass"\]\[data-material="acrylic"\]\[data-focused="false"\] \{ --shell: #0a0a0a; \}/);
  assert.doesNotMatch(tokens, /data-material="accent"/);
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
  // Windows camında hafif tül; yedek Acrylic daha açık çizdiği için orada koyu kalır.
  assert.match(tokens, /--tint:\s*0\.2;/);
  assert.match(tokens, /:root\[data-theme="glass"\]\[data-material="acrylic"\] \{ --tint: 0\.34; \}/);
});

test("camda kartlar glassmorphism: ışık süzmesi ve parlak üst kenar; hover süzmeyi silmez", async () => {
  const tokens = await read("tokens.css");
  const glass = tokens.slice(tokens.indexOf(":root {"), tokens.indexOf("}", tokens.indexOf(":root {")));
  assert.match(glass, /--card-bg: linear-gradient\(/);
  assert.match(glass, /--card-shadow: inset 0 1px 0 rgba\(255, 255, 255/);
  assert.match(glass, /--kart-bg: linear-gradient\(/);
  // Kısaltılmış "background" hover'da gradyanı silerdi: kart düzleşip parlar.
  assert.match(await read("screens.css"), /\.kart:hover \{[^}]*background-color:/);
  assert.match(await read("management.css"), /\.resource-card:hover \{[^}]*background-color:/);
});

test("saydamlık kapalıysa cam opak", async () => {
  assert.match(await read("base.css"), /\.shell \{[^}]*background: var\(--shell\)/);
  assert.match(await read("tokens.css"), /@media \(prefers-reduced-transparency: reduce\) \{\s*:root\[data-theme="glass"\] \{ --shell: #0a0a0a; \}/);
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

test("kenar çubuğunda kısayol ipucu ve kullanıcı adı yok; sürüm altta", async () => {
  const rail = await readFile(join(rendererDir, "components", "Rail.tsx"), "utf8");
  assert.doesNotMatch(rail, /kbd|Ctrl|⌘|userName/);
  assert.match(rail, /className="rail-foot"[\s\S]*Kadran[\s\S]*data-testid="rail-version"/);
  assert.doesNotMatch(rail.slice(rail.indexOf('className="rail-brand"'), rail.indexOf('className="search"')), /rail-version/);
});

test("pano yok: uygulama Sunucular ekranında açılır", async () => {
  const rail = await readFile(join(rendererDir, "components", "Rail.tsx"), "utf8");
  assert.doesNotMatch(rail, /dashboard/);
  const app = await readFile(join(rendererDir, "App.tsx"), "utf8");
  assert.match(app, /useState<FleetView>\("servers"\)/);
  assert.doesNotMatch(app, /Dashboard|"dashboard"/);
});

test("metin karakteri simge yok", async () => {
  const apps = await readFile(join(rendererDir, "screens", "AppsScreen.tsx"), "utf8");
  assert.doesNotMatch(apps, /◇/);
});

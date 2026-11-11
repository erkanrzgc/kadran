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

test("Coolify ölçüleri: kart 12, denetim 8, küçük 6 px; kenar çubuğu 200 px", async () => {
  const tokens = await read("tokens.css");
  assert.match(tokens, /--radius-card:\s*12px/);
  assert.match(tokens, /--radius:\s*8px/);
  assert.match(tokens, /--radius-sm:\s*6px/);
  const layout = await read("layout.css");
  // Coolify'da 224; kısa menü yazılarına geniş geldi (kullanıcı, 6 Kasım).
  assert.match(layout, /\.rail \{[^}]*width: 200px/);
});

test("üst çubuk düz: zemin, gölge, yüzme yok; düğmeler sağ köşede", async () => {
  const layout = await read("layout.css");
  const bar = layout.slice(layout.indexOf(".topbar {"), layout.indexOf("}", layout.indexOf(".topbar {")));
  // İnce şerit, Windows 11 başlığı kadar (kullanıcı, 6 Kasım); panel hemen altından başlar.
  assert.match(bar, /height: 32px/);
  assert.doesNotMatch(bar, /position: absolute|background|box-shadow|border-radius|filter/);
  // KADRAN panelin üst kenarının 16 px altından başlar: şerit (32) + 16 (kullanıcı, 6 Kasım).
  const brand = layout.slice(layout.indexOf(".rail-brand {"), layout.indexOf("}", layout.indexOf(".rail-brand {")));
  assert.match(brand, /padding: 48px 16px 0;/);
  assert.match(brand, /align-items: flex-start;/);
  assert.doesNotMatch(layout, /\.is-mac \.rail-brand/);
  assert.match(layout, /\.win-btns \{[^}]*margin-left: auto/);
  assert.doesNotMatch(layout, /\.lens|--bar-/);
  assert.doesNotMatch(await read("tokens.css"), /--bar-/);
});

test("kenar çubuğunda dolgu ve mor yok; seçili satırın camı kayar, fareyi kayan vurgu izler (7, K-163)", async () => {
  const layout = await read("layout.css");
  const rail = layout.slice(layout.indexOf(".rail {"), layout.indexOf("}", layout.indexOf(".rail {")));
  assert.doesNotMatch(rail, /background/);
  assert.match(layout, /\.nav-item \{[^}]*border-radius: var\(--radius\);/);
  // Satırların arkasında tek gösterge: seçilen satıra kayar (büyüyerek solma yok).
  const rule = (sel: string): string => {
    const at = layout.indexOf(`${sel} {`);
    assert.ok(at >= 0, sel);
    return layout.slice(at, layout.indexOf("}", at));
  };
  assert.match(rule(".nav-pill"), /background: var\(--pill\); box-shadow: var\(--pill-shadow\);/);
  assert.match(rule(".nav-hover"), /background: var\(--flat\);/);
  // İlk yerleşim kaymaz: kayma yalnız gösterge yerine oturduktan sonra (data-ready).
  assert.doesNotMatch(rule(".nav-pill"), /transition/);
  assert.doesNotMatch(rule(".nav-hover"), /transition: transform/);
  assert.match(rule(".nav-pill[data-ready]"), /transition: transform/);
  assert.match(rule(".nav-hover[data-ready]"), /transition: transform/);
  assert.doesNotMatch(layout, /scale\(/);
  // Satırın kendisi zemin almaz; zemini gösterge verir (yoksa iki zemin üst üste biner).
  assert.doesNotMatch(layout, /\.nav-item[^{]*:hover \{[^}]*background/);
  assert.doesNotMatch(layout, /\.nav-item::before|\.nav-item\.is-active::before/);
  assert.match(await read("tokens.css"), /--pill: linear-gradient\(180deg,/);
  assert.doesNotMatch(layout, /7317ff|5a12c7|6b16ed|120e1a/);
  // İki menü de göstergeyi kullanır.
  for (const file of ["Rail.tsx", "ServerSubnav.tsx"]) {
    assert.match(await readFile(join(rendererDir, "components", file), "utf8"), /<NavSlider activeKey=/, file);
  }
});

const THEME_BLOCKS = [':root[data-theme="dark"]', ':root[data-theme="light"]'];

function block(tokens: string, selector: string): string {
  const start = tokens.indexOf(`${selector} {`);
  assert.ok(start >= 0, selector);
  return tokens.slice(start, tokens.indexOf("}", start));
}

test("iki tema da aynı renk adlarını tanımlıyor; bileşende sabit beyaz ya da siyah yok", async () => {
  const tokens = await read("tokens.css");
  const named = (css: string): string[] => [...css.matchAll(/(--[a-z-]+):/g)].map((m) => m[1]!);
  for (const selector of THEME_BLOCKS) {
    const own = named(block(tokens, selector));
    for (const name of ["--frame", "--frame-solid", "--sheet", "--sheet-edge", "--sheet-shadow", "--ov", "--fg", "--fg-dim", "--fg-faint", "--line", "--edge", "--flat", "--menu", "--card-bg", "--well-bg", "--pill", "--text-shadow", "--kart-bg", "--kart-shadow"]) {
      assert.ok(own.includes(name), `${selector}: ${name}`);
    }
  }
  assert.match(block(tokens, ':root[data-theme="light"]'), /color-scheme: light/);
  for (const [name, css] of await styles()) {
    if (name === "tokens.css") continue;
    assert.doesNotMatch(css, /rgba\(255, 255, 255|rgba\(0, 0, 0|#1b1b1f|#16161a|#f2f2f2/, name);
  }
});

test("cam yoksa çerçeve opak: malzemesiz sistemde ve odak gidince yedek Acrylic'te; Windows camı açık kalır", async () => {
  const tokens = await read("tokens.css");
  assert.match(tokens, /:root \{[^}]*--shell: var\(--frame\);/);
  assert.match(tokens, /:root\[data-material="none"\] \{ --shell: var\(--frame-solid\); \}/);
  assert.match(tokens, /:root\[data-material="acrylic"\]\[data-focused="false"\] \{ --shell: var\(--frame-solid\); \}/);
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

test("pencere modu, ton ve ayrı cam teması kalmadı", async () => {
  for (const [name, css] of await styles()) {
    assert.doesNotMatch(css, /data-mode|data-tone|--panel|--glow|--tint|data-theme="glass"/, name);
  }
});

test("cam yalnız çerçevede: kenar çubuğu çizgisiz, içerik opak panelde (K-163)", async () => {
  const layout = await read("layout.css");
  const rail = layout.slice(layout.indexOf(".rail {"), layout.indexOf("}", layout.indexOf(".rail {")));
  assert.doesNotMatch(rail, /border-right/);
  // Boşluk panelin dışında; üst çubuk tam genişlik kalır, kapat düğmesi pencerenin köşesinde.
  assert.match(layout, /\.main > \.stage, \.main > \.page-host \{ margin: 0 var\(--frame-gap\) var\(--frame-gap\) 0; \}/);
  const main = layout.slice(layout.indexOf(".main {"), layout.indexOf("}", layout.indexOf(".main {")));
  assert.doesNotMatch(main, /padding/);
  assert.match(layout, /\.page-host \{[^}]*gap: var\(--frame-gap\);/);
  assert.match(
    layout,
    /\.main > \.stage, \.page-host > \.subnav, \.page-host > \.stage \{ background: var\(--sheet\); border: 1px solid var\(--sheet-edge\); border-radius: var\(--sheet-radius\); box-shadow: var\(--sheet-shadow\); \}/,
  );
  // Dar pencerede ikinci menü üste geçer; kendi kenarlığını silerse panel kenarsız kalır.
  const narrow = layout.slice(layout.indexOf("@media (max-width: 1050px)"));
  const narrowSubnav = narrow.slice(narrow.indexOf(".subnav {"), narrow.indexOf("}", narrow.indexOf(".subnav {")));
  assert.ok(narrowSubnav.length > 0);
  assert.doesNotMatch(narrowSubnav, /border/);
  const tokens = await read("tokens.css");
  assert.match(tokens, /--sheet-radius:\s*10px/);
  assert.match(tokens, /--frame-gap:\s*8px/);
  for (const selector of THEME_BLOCKS) {
    const css = block(tokens, selector);
    // Panel ve opak çerçeve saydamlık taşımaz: içerik masaüstünün üstünde okunur kalır.
    assert.match(css, /--sheet: #[0-9a-f]{6};/, selector);
    assert.match(css, /--frame-solid: #[0-9a-f]{6};/, selector);
  }
  // Kullanıcının seçtiği örtüler (6 Kasım): açıkta Windows camının beyaz tonu yeter, koyuda hafif örtü.
  assert.match(block(tokens, ':root[data-theme="light"]'), /--frame: rgb\(244 244 247 \/ 0%\);/);
  assert.match(block(tokens, ':root[data-theme="dark"]'), /--frame: rgb\(22 22 24 \/ 20%\);/);
});

test("düğmeler zıplamaz: üstüne gelince ve basınca yer değiştirmez, yalnız renk geçer", async () => {
  const controls = await read("controls.css");
  const btnRules = [...controls.matchAll(/\.btn[^{]*\{[^}]*\}/g)].map((m) => m[0]);
  assert.ok(btnRules.length > 0);
  for (const rule of btnRules) assert.doesNotMatch(rule, /transform|translate/, rule.slice(0, 60));
});

test("tema geçişinde animasyon yok: renkler tek seferde döner", async () => {
  // Menü satırı ve düğme renkleri 120 ms'de, paneller anında dönüyordu: ekran parça parça değişiyordu.
  assert.match(await read("base.css"), /:root\.theme-switching \*, :root\.theme-switching \*::before, :root\.theme-switching \*::after \{ transition: none !important; \}/);
  const provider = await readFile(join(rendererDir, "appearance", "AppearanceProvider.tsx"), "utf8");
  assert.match(provider, /saveDelay\(/);
  // Panel camdan geç dönüyordu: renkler tıklandığı an sayfaya verilir, kayıt (cam) hemen
  // gider, arayüzün yeniden çizimi en sona kalır.
  const update = provider.slice(provider.indexOf("const update = useCallback"));
  const flip = update.indexOf("switchTheme(");
  const saved = update.indexOf("save(next)", flip);
  const render = update.indexOf("startTransition(", flip);
  assert.ok(flip >= 0, "tema değişince renkler update içinde, hemen verilir");
  assert.ok(saved > flip, "kayıt (cam) renklerden sonra, hemen");
  assert.ok(render > saved, "arayüzün yeniden çizimi en sonda");
});

test("saydamlık kapalıysa çerçeve opak", async () => {
  assert.match(await read("base.css"), /\.shell \{[^}]*background: var\(--shell\)/);
  const tokens = await read("tokens.css");
  assert.match(tokens, /@media \(prefers-reduced-transparency: reduce\) \{\s*:root \{ --shell: var\(--frame-solid\); \}/);
  // Seçiciler eşit güçte: kural ancak varsayılan çerçeveden SONRA gelirse kazanır.
  assert.ok(tokens.indexOf("@media (prefers-reduced-transparency") > tokens.indexOf("--shell: var(--frame);"));
});

test("yüzen yüzey opak: menü, kutu, bildirim", async () => {
  const tokens = await read("tokens.css");
  // Saydamlık payı ("/ %") olmayan renk: menü camın üstünde de okunur.
  for (const selector of THEME_BLOCKS) assert.match(block(tokens, selector), /--menu:\s*(oklch|rgb)\([^/)]*\);/, selector);
  assert.match(await read("layout.css"), /\.crumb-menu \{[^}]*background: var\(--menu\)/);
  assert.match(await read("management.css"), /\.operation-dialog \{[^}]*background: var\(--menu\)/);
  assert.match(await read("controls.css"), /\.toast \{[^}]*background: var\(--menu\)/);
});

test("sürükleme bölgesi düğmeleri ve açılan menüyü yutmaz", async () => {
  const layout = await read("layout.css");
  assert.match(layout, /\.rail-brand, \.topbar \{ --wails-draggable: drag; \}/);
  assert.match(layout, /button, input, select, a, summary, \.crumb-menu, \.operation-dialog, \.toast \{ --wails-draggable: no-drag; \}/);
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

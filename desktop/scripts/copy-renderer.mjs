/**
 * Derlenmeyen varlıkları out/ altına kopyalar.
 *
 * İki grup:
 *
 *   - renderer/ — html, css, js. Bilerek derlenmiyor (bkz. tsconfig.json):
 *     tek ekranlık bir arayüz için tarayıcı tarafında modül çözümleme
 *     karmaşıklığına değmez.
 *   - preload/index.cjs — CommonJS olmak ZORUNDA. `sandbox: true` iken
 *     Electron ESM preload çalıştırmıyor ve package.json'daki
 *     "type": "module" her .js dosyasını ESM yapardı.
 */

import { cp, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, "..", "src");
const out = join(here, "..", "out");

const assets = [
  { from: join(src, "renderer"), to: join(out, "renderer"), recursive: true },
  { from: join(src, "preload", "index.cjs"), to: join(out, "preload", "index.cjs") },
];

for (const asset of assets) {
  await mkdir(asset.recursive ? asset.to : dirname(asset.to), { recursive: true });
  await cp(asset.from, asset.to, { recursive: Boolean(asset.recursive) });
  console.log(`kopyalandı: ${asset.from} → ${asset.to}`);
}

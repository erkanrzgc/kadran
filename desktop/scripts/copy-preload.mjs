/**
 * Preload'u out/ altına kopyalar.
 *
 * preload/index.cjs CommonJS olmak ZORUNDA: `sandbox: true` iken Electron
 * ESM preload çalıştırmıyor ve package.json'daki "type": "module" her .js
 * dosyasını ESM yapardı. Arayüz artık Vite ile derleniyor; bu betik
 * out/renderer'a DOKUNMAZ (dokunursa Vite çıktısını ezer).
 */

import { cp, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const from = join(here, "..", "src", "preload", "index.cjs");
const to = join(here, "..", "out", "preload", "index.cjs");

await mkdir(dirname(to), { recursive: true });
await cp(from, to);
console.log(`kopyalandı: ${from} → ${to}`);

/**
 * lucide-animated ikonlarını (pqoqubbw/icons, MIT; çizimler Lucide, ISC)
 * SABİT bir commit'ten kopyalar ve projeye uyarlar:
 *
 *   - "use client" ve shadcn'in `cn` içe aktarması kalkar (`cn` yerine
 *     düz className);
 *   - `React.MouseEvent` → içe aktarılan `MouseEvent` tipi;
 *   - başa kaynak ve lisans başlığı eklenir.
 *
 * Yeniden çalıştırmak aynı çıktıyı verir. İkon eklemek için NAMES'e ekle,
 * betiği çalıştır, test/icons.test.ts'teki EXPECTED'i güncelle.
 */

import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const COMMIT = "072c38b1b04ea738d90a084485ccaad4b890ddca";
const NAMES = ["activity", "shield-check", "settings", "refresh-cw", "server", "copy", "check", "chevron-down", "history", "badge-alert"];
const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", "src", "renderer", "icons");

async function fetchText(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return res.text();
}

function adapt(name, src) {
  let s = src;
  s = s.replace(/^"use client";\s*\n/m, "");
  s = s.replace(/^import \{ cn \} from "@\/lib\/utils";\s*\n/m, "");
  s = s.replace(/className=\{cn\(className\)\}/g, "className={className}");
  if (/\bReact\.MouseEvent\b/.test(s)) {
    s = s.replace(/\bReact\.MouseEvent\b/g, "MouseEvent");
    s = s.replace(/^(import type \{ HTMLAttributes) \} from "react";/m, '$1, MouseEvent } from "react";');
  }
  if (/@\/lib\/utils|\bcn\(/.test(s)) throw new Error(`${name}: cn kaldırılamadı`);
  if (/\bReact\./.test(s)) throw new Error(`${name}: React. başvurusu kaldı`);
  const header =
    `// Kaynak: pqoqubbw/icons@${COMMIT} icons/${name}.tsx (MIT).\n` +
    `// Çizim Lucide'den (ISC). Lisanslar: ./LICENSE. Uyarlama: scripts/vendor-icons.mjs\n\n`;
  return header + s;
}

function exportName(src) {
  const m = src.match(/export \{ (\w+Icon) \};/);
  if (!m) throw new Error("dışa aktarım bulunamadı");
  return m[1];
}

await mkdir(outDir, { recursive: true });
const exports = [];
for (const name of NAMES) {
  const src = await fetchText(`https://raw.githubusercontent.com/pqoqubbw/icons/${COMMIT}/icons/${name}.tsx`);
  const adapted = adapt(name, src);
  await writeFile(join(outDir, `${name}.tsx`), adapted, "utf8");
  exports.push(`export { ${exportName(adapted)} } from "./${name}.tsx";`);
}
await writeFile(join(outDir, "index.ts"), exports.join("\n") + "\n", "utf8");

const mit = await fetchText(`https://raw.githubusercontent.com/pqoqubbw/icons/${COMMIT}/LICENSE`);
const isc = await fetchText("https://raw.githubusercontent.com/lucide-icons/lucide/main/LICENSE");
await writeFile(
  join(outDir, "LICENSE"),
  `Animasyonlu ikonlar: pqoqubbw/icons (${COMMIT})\n\n${mit.trim()}\n\n---\n\nÇizimler: Lucide\n\n${isc.trim()}\n`,
  "utf8",
);
console.log(`${NAMES.length} ikon kopyalandı → ${outDir}`);

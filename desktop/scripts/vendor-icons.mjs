/**
 * lucide-animated ikonlarını (pqoqubbw/icons, MIT; çizimler Lucide, ISC)
 * SABİT commit'lerden kopyalar ve projeye uyarlar:
 *
 *   - "use client" ve shadcn'in `cn` içe aktarması kalkar (`cn` yerine
 *     düz className);
 *   - `React.MouseEvent` → içe aktarılan `MouseEvent` tipi;
 *   - başa kaynak ve lisans başlığı eklenir.
 *
 * İki aşamalı: 1. aşama tüm kaynakları doğrular (hiçbir dosya yazılmaz),
 * 2. aşama tüm dosyaları yazar. Başarısız kapatma: hatada sıfır değişiklik.
 *
 * Yeniden çalıştırmak aynı çıktıyı verir. İkon eklemek için NAMES'e ekle,
 * betiği çalıştır, test/icons.test.ts'teki EXPECTED'i güncelle.
 */

import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { verifySha256, validateKeySet } from "./vendor-integrity.mjs";

const COMMIT = "072c38b1b04ea738d90a084485ccaad4b890ddca";
const LUCIDE_COMMIT = "b56741cf30c08fc7248bf41ddd3a261367aa9579";
const NAMES = ["activity", "shield-check", "settings", "refresh-cw", "server", "copy", "check", "chevron-down", "history", "badge-alert"];
// Yukarı akış ikon adları proje sözleşmesinden farklıysa takma ad verme
const EXPORT_ALIASES = {
  RefreshCWIcon: "RefreshCwIcon",
};
// Kaynak bütünlüğü doğrulama: sabitlenmiş commit'ler GitHub'da değişmezdir;
// bu özetler aktarım hatasını veya kaynak değişikliğini algılayarak başarısız kapatmayı sağlar.
// COMMIT veya LUCIDE_COMMIT değiştirildiğinde güncelleyin.
const EXPECTED_SHA256 = {
  "activity": "aac1c5b9b02a2b8dbb5cbd532fa4c1ce6fad110da36e80526970147b6dafab80",
  "shield-check": "7fe067dfcc345616d5723c35c3a4db3c9793b5e4c82537f30e208b46ba908fc3",
  "settings": "dccbe307789095c85bd7847c383360e721bd235c42c95c069d6fc28b0fde22bc",
  "refresh-cw": "a6d4495667c8cac9b264d797b8aa228bb6bd5233cef501f61de67e4d08310102",
  "server": "dc9a054cd366df9dd6600f2edc08a3b6928015a2d8b37eee23119c5dc390b5fa",
  "copy": "8854d8eac44a73c18c3af656348f9a4f6c0920499cbba63c33288047cf54c052",
  "check": "59fb298bbff88ab0900d718ec16ee4da46489ad2f75289d59ff2bcb28b7e79bf",
  "chevron-down": "0b7507c689924b6c241b76d3f1b3db759aebc0f03477f12d78f5abf7b1fc4337",
  "history": "bb6b022562f32036e7050e1516b718e63a0ad437ca1e0161ba1f6b3ef7d60ba8",
  "badge-alert": "7f878a3b03c567e8afee85bb1dbce7f095604a06992aa43dcf78b5e72d602307",
  "LICENSE:pqoqubbw": "b77c01c81935b05f8f5ec43652cd6a29675e1b1b7505ac66cd9ac1f4ef4ac3ef",
  "LICENSE:lucide": "b495047bd93a9b06913511076f504daba17d5bbeb3e0650f3bb53a4220329c57",
};
const here = dirname(fileURLToPath(import.meta.url));
const outDir = join(here, "..", "src", "renderer", "icons");

// Anahtar seti doğrula (erken hata)
validateKeySet(NAMES, EXPECTED_SHA256);

async function fetchBytes(url) {
  const res = await fetch(url);
  if (!res.ok) throw new Error(`${url}: HTTP ${res.status}`);
  return Buffer.from(await res.arrayBuffer());
}

// Özet, çözülmeden önceki ham baytlardan alınır; metne ancak doğrulamadan
// sonra çevrilir.
async function fetchAndVerify(url, expectedKey) {
  const buf = await fetchBytes(url);
  verifySha256(buf, expectedKey, EXPECTED_SHA256);
  return buf.toString("utf8");
}

function adapt(name, src) {
  let s = src;
  s = s.replace(/^"use client";\s*\n/m, "");
  s = s.replace(/^import \{ cn \} from "@\/lib\/utils";\s*\n/m, "");
  s = s.replace(/className=\{cn\(className\)\}/g, "className={className}");
  // Tailwind overflow-visible sınıfını SVG özelliğine dönüştür
  s = s.replace(/className="overflow-visible"/g, 'overflow="visible"');
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

// ====== 1. aşama: tüm kaynakları doğrula (dosya yazılmaz) ======
console.log("Aşama 1: Kaynaklar doğrulanıyor...");

const icons = [];
for (const name of NAMES) {
  const src = await fetchAndVerify(
    `https://raw.githubusercontent.com/pqoqubbw/icons/${COMMIT}/icons/${name}.tsx`,
    name
  );
  const adapted = adapt(name, src);
  icons.push({ name, adapted });
}

const mit = await fetchAndVerify(
  `https://raw.githubusercontent.com/pqoqubbw/icons/${COMMIT}/LICENSE`,
  "LICENSE:pqoqubbw"
);
const isc = await fetchAndVerify(
  `https://raw.githubusercontent.com/lucide-icons/lucide/${LUCIDE_COMMIT}/LICENSE`,
  "LICENSE:lucide"
);

// İçeriği bellekte oluştur
const exports = [];
for (const { name, adapted } of icons) {
  const originalName = exportName(adapted);
  const alias = EXPORT_ALIASES[originalName];
  if (alias) {
    exports.push(`export { ${originalName} as ${alias} } from "./${name}.tsx";`);
  } else {
    exports.push(`export { ${originalName} } from "./${name}.tsx";`);
  }
}
const indexContent = exports.join("\n") + "\n";
const licenseContent =
  `Animasyonlu ikonlar: pqoqubbw/icons (${COMMIT})\n\n${mit.trim()}\n\n---\n\nÇizimler: Lucide (${LUCIDE_COMMIT})\n\n${isc.trim()}\n`;

console.log("✓ Aşama 1 başarılı: tüm kaynaklar doğrulandı");

// ====== 2. aşama: tüm dosyaları yaz ======
console.log("Aşama 2: Dosyalar yazılıyor...");

await mkdir(outDir, { recursive: true });

for (const { name, adapted } of icons) {
  await writeFile(join(outDir, `${name}.tsx`), adapted, "utf8");
}
await writeFile(join(outDir, "index.ts"), indexContent, "utf8");
await writeFile(join(outDir, "LICENSE"), licenseContent, "utf8");

console.log(`${NAMES.length} ikon kopyalandı → ${outDir}`);

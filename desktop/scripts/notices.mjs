/**
 * build/THIRD_PARTY_NOTICES.txt'i üretir: Kadran'ın telif satırı ve lisansı,
 * arayüze gömülen her çalışma zamanı paketinin lisans metni, Geist (OFL) ve
 * Coolify'dan uyarlanan tasarım dili (Apache-2.0). Paketlenmiş uygulamanın
 * resources/ dizinine konur. Electron'un kendi lisanslarını (LICENSE,
 * LICENSES.chromium.html) @electron/packager ekliyor.
 *
 * Lisans dosyası bulunamayan bir paket varsa betik DURUR.
 */

import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, "..");
const repo = join(desktop, "..");
const out = join(desktop, "build", "THIRD_PARTY_NOTICES.txt");

async function licenseText(dir) {
  const name = (await readdir(dir)).find((f) => /^licen[cs]e/i.test(f));
  if (!name) throw new Error(`lisans dosyası yok: ${dir}`);
  return readFile(join(dir, name), "utf8");
}

const lock = JSON.parse(await readFile(join(desktop, "package-lock.json"), "utf8"));
const sections = [
  `KΛDRΛN (Kadran) — Apache-2.0\nCopyright 2026 erkanrzgc\n\n${(await readFile(join(repo, "LICENSE"), "utf8")).trim()}`,
];
for (const [path, p] of Object.entries(lock.packages)) {
  if (path === "" || p.dev || p.devOptional) continue;
  const name = path.replace(/^.*node_modules\//, "");
  sections.push(`${name}@${p.version} — ${p.license}\n\n${(await licenseText(join(desktop, path))).trim()}`);
}
sections.push(`Geist and Geist Mono — OFL-1.1\n\n${(await readFile(join(desktop, "src", "renderer", "fonts", "OFL.txt"), "utf8")).trim()}`);
sections.push(`Coolify (https://github.com/coollabsio/coolify) — Apache-2.0\nCopyright coolLabs\n\nThe desktop layout, measurements and component anatomy are adapted from Coolify's design\nlanguage (DESIGN.md). Colors, icons and the logo are Kadran's own. The license text is the\nApache License 2.0 reproduced in the first section of this file.`);

await mkdir(dirname(out), { recursive: true });
await writeFile(out, sections.join(`\n\n${"=".repeat(72)}\n\n`) + "\n", "utf8");
console.log(`${sections.length} bölüm → ${out}`);

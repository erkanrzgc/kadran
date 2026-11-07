/**
 * THIRD_PARTY_NOTICES.txt'i üretir: Kadran'ın telif satırı ve lisansı, ikiliye
 * bağlanan her Go modülünün ve Go'nun kendi lisansı, arayüze gömülen her
 * çalışma zamanı paketinin lisans metni, Geist (OFL) ve Coolify'dan uyarlanan
 * tasarım dili (Apache-2.0). Paketlenmiş uygulamanın yanına konur
 * (scripts/dist.ts). WebView2 Windows'la geliyor, pakete girmiyor.
 *
 * Lisans dosyası bulunamayan bir paket ya da modül varsa betik DURUR.
 *
 * Kullanım: node scripts/notices.mjs [çıktı yolu]
 */

import { execFileSync } from "node:child_process";
import { existsSync } from "node:fs";
import { mkdir, readdir, readFile, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, "..");
const repo = join(desktop, "..");
const out = process.argv[2] ?? join(desktop, "build", "THIRD_PARTY_NOTICES.txt");

async function licenseText(dir) {
  const name = (await readdir(dir)).find((f) => /^(licen[cs]e|copying)/i.test(f));
  if (!name) throw new Error(`lisans dosyası yok: ${dir}`);
  return readFile(join(dir, name), "utf8");
}

const sections = [
  `KΛDRΛN (Kadran) — Apache-2.0\nCopyright 2026 erkanrzgc\n\n${(await readFile(join(repo, "LICENSE"), "utf8")).trim()}`,
];

// Go: ikiliye giren modüller (Wails etiketleriyle), deponun kendisi hariç.
const go = (...args) => execFileSync("go", args, { cwd: desktop, encoding: "utf8" }).trim();
sections.push(`Go — BSD-3-Clause\n\n${(await readFile(join(go("env", "GOROOT"), "LICENSE"), "utf8")).trim()}`);
const modules = new Map();
for (const line of go("list", "-deps", "-tags", "desktop,production", "-f", "{{with .Module}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}", ".").split("\n")) {
  const [path, version, dir] = line.split("|");
  if (!path || path.startsWith("github.com/erkanrzgc/kadran")) continue;
  modules.set(path, { version, dir });
}
for (const [path, m] of [...modules].sort(([a], [b]) => a.localeCompare(b))) {
  sections.push(`${path}@${m.version}\n\n${(await licenseText(m.dir)).trim()}`);
}

// Arayüz: package-lock'ta dev olmayan paketler.
const lock = JSON.parse(await readFile(join(desktop, "package-lock.json"), "utf8"));
for (const [path, p] of Object.entries(lock.packages)) {
  if (path === "" || p.dev || p.devOptional) continue;
  // Başka platformların isteğe bağlı paketleri bu makineye kurulmaz, pakete de girmez.
  if (p.optional && !existsSync(join(desktop, path))) continue;
  const name = path.replace(/^.*node_modules\//, "");
  sections.push(`${name}@${p.version} — ${p.license}\n\n${(await licenseText(join(desktop, path))).trim()}`);
}
sections.push(`Geist and Geist Mono — OFL-1.1\n\n${(await readFile(join(desktop, "src", "renderer", "fonts", "OFL.txt"), "utf8")).trim()}`);
sections.push(`Coolify (https://github.com/coollabsio/coolify) — Apache-2.0\nCopyright coolLabs\n\nThe desktop layout, measurements and component anatomy are adapted from Coolify's design\nlanguage (DESIGN.md). Colors, icons and the logo are Kadran's own. The license text is the\nApache License 2.0 reproduced in the first section of this file.`);

await mkdir(dirname(out), { recursive: true });
await writeFile(out, sections.join(`\n\n${"=".repeat(72)}\n\n`) + "\n", "utf8");
console.log(`${sections.length} bölüm (${modules.size} Go modülü) → ${out}`);

/**
 * Masaüstü paketini üretir (imzasız, Windows, K-165):
 *
 *   dist/Kadran.exe                 tek ikili: Wails kabuğu + gömülü arayüz
 *   dist/THIRD_PARTY_NOTICES.txt    scripts/notices.mjs
 *
 * Önce `npm run build` (arayüz out/renderer'a). Sürüm damgası CLI'ınkiyle
 * aynı: etiket + çalışma ağacı durumu (scripts/build-linux.sh).
 */

import { execFileSync } from "node:child_process";
import { readFileSync, statSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = resolve(here, "..");
const repo = resolve(desktop, "..");
const exe = join(desktop, "dist", "Kadran.exe");
/** Bütçe (filo tasarımı): paket ≤ 20 MB. */
const MAX_BYTES = 20_000_000;

if (process.platform !== "win32") throw new Error("the desktop package is built on Windows only (K-165)");

const git = (...args: string[]) => execFileSync("git", args, { cwd: repo, encoding: "utf8" }).trim();
const version = git("describe", "--tags", "--always", "--dirty");
const commit = git("rev-parse", "HEAD").slice(0, 12);
// Modül yolu go.mod'dan: yanlış bir -X yolu HATASIZ yok sayılır ve ikili "dev" der (K-130).
const mod = execFileSync("go", ["list", "-m"], { cwd: repo, encoding: "utf8" }).trim();
const ldflags = `-H windowsgui -s -w -X ${mod}/internal/version.Version=${version} -X ${mod}/internal/version.Commit=${commit}`;

execFileSync("go", ["build", "-tags", "desktop,production", "-trimpath", "-ldflags", ldflags, "-o", exe, "."], {
  cwd: desktop,
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0", GOOS: "windows", GOARCH: "amd64" },
});

// Damga ölçülür, varsayılmaz: ikili sürüm metnini taşımalı. -X yolu
// tutmasaydı metin hiç girmez, arayüz "KΛDRΛN dev" gösterirdi.
const binary = readFileSync(exe);
for (const stamp of [version, commit]) {
  if (!binary.includes(Buffer.from(stamp, "utf8"))) throw new Error(`the binary is not stamped with ${stamp}`);
}
const size = statSync(exe).size;
if (size > MAX_BYTES) throw new Error(`the package is ${size} bytes, over the ${MAX_BYTES} budget`);
console.log(`Kadran.exe ${version} (${commit}), ${(size / 1e6).toFixed(1)} MB`);

execFileSync(process.execPath, [join(here, "notices.mjs"), join(desktop, "dist", "THIRD_PARTY_NOTICES.txt")], { stdio: "inherit" });

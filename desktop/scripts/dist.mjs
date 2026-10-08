/**
 * Bu makinenin platformu için paket klasörü üretir (imzasız):
 *
 *   dist/Kadran-<platform>-x64/
 *     Kadran(.exe)                       pencere başlığı KΛDRΛN
 *     resources/kadran(.exe)             sidecar (go build)
 *     resources/THIRD_PARTY_NOTICES.txt  scripts/notices.mjs
 *
 * Electron paket adı "kadran-desktop" DEĞİŞMEZ: kullanıcı verisi dizini
 * ondan türüyor (K-136); paket yalnız çalıştırılabilirin adını Kadran yapar.
 * Uygulamanın kendisi out/ ve package.json'dan ibaret: arayüzün çalışma
 * zamanı bağımlılıkları Vite ile gömülü, ana süreç yalnız Electron ve Node
 * kullanıyor; node_modules pakete girmez.
 */

import { execFileSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { packager } from "@electron/packager";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = join(here, "..");
const repo = join(desktop, "..");
const exe = process.platform === "win32" ? "kadran.exe" : "kadran";
const sidecar = join(desktop, "build", "sidecar", exe);
const goos = { win32: "windows", darwin: "darwin", linux: "linux" }[process.platform];
if (!goos) throw new Error(`desteklenmeyen platform: ${process.platform}`);

execFileSync("go", ["build", "-trimpath", "-o", sidecar, "./cmd/kadran"], {
  cwd: repo,
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0", GOOS: goos, GOARCH: "amd64" },
});
execFileSync(process.execPath, [join(here, "notices.mjs")], { stdio: "inherit" });

const paths = await packager({
  dir: desktop,
  out: join(desktop, "dist"),
  platform: process.platform,
  arch: "x64",
  name: "Kadran",
  executableName: "Kadran",
  overwrite: true,
  asar: true,
  prune: false,
  extraResource: [sidecar, join(desktop, "build", "THIRD_PARTY_NOTICES.txt")],
  ignore: (path) => path !== "" && path !== "/package.json" && !path.startsWith("/out"),
});
console.log(`paket: ${paths.join(", ")}`);

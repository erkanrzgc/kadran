/**
 * Bu makinenin platformu için paket klasörü üretir (imzasız):
 *
 *   dist/Kadran-<platform>-x64/
 *     Kadran(.exe)                       pencere başlığı KΛDRΛN
 *     resources/kadran(.exe)             sidecar (go build, sürüm damgalı)
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

const git = (...args) => execFileSync("git", args, { cwd: repo, encoding: "utf8" }).trim();
// scripts/build-linux.sh ile aynı damga: etiket + çalışma ağacı durumu.
const version = git("describe", "--tags", "--always", "--dirty");
const commit = git("rev-parse", "HEAD").slice(0, 12);
// Modül yolu go.mod'dan: yanlış bir -X yolu HATASIZ yok sayılır ve ikili "dev" der (K-130).
const mod = execFileSync("go", ["list", "-m"], { cwd: repo, encoding: "utf8" }).trim();
const ldflags = `-s -w -X ${mod}/internal/version.Version=${version} -X ${mod}/internal/version.Commit=${commit}`;

execFileSync("go", ["build", "-trimpath", "-ldflags", ldflags, "-o", sidecar, "./cmd/kadran"], {
  cwd: repo,
  stdio: "inherit",
  env: { ...process.env, CGO_ENABLED: "0", GOOS: goos, GOARCH: "amd64" },
});

// Damga ölçülür, varsayılmaz: sidecar'a sürüm sorulur. "dev" dönerse -X
// yolu tutmamıştır ve arayüz "KΛDRΛN dev" gösterirdi.
const reply = JSON.parse(
  execFileSync(sidecar, ["sidecar"], {
    input: `${JSON.stringify({ jsonrpc: "2.0", id: 1, method: "version", params: {} })}\n`,
    encoding: "utf8",
    timeout: 20_000,
  }).split("\n")[0],
);
if (reply.result?.version !== version) {
  throw new Error(`sidecar sürümü damgalanmadı: beklenen ${version}, gelen ${JSON.stringify(reply)}`);
}
console.log(`sidecar ${version} (${commit})`);

execFileSync(process.execPath, [join(here, "notices.mjs")], { stdio: "inherit" });

const paths = await packager({
  dir: desktop,
  out: join(desktop, "dist"),
  platform: process.platform,
  arch: "x64",
  name: "Kadran",
  executableName: "Kadran",
  icon: process.platform === "win32" ? join(desktop, "assets", "kadran.ico") : undefined,
  overwrite: true,
  asar: true,
  prune: false,
  extraResource: [sidecar, join(desktop, "build", "THIRD_PARTY_NOTICES.txt")],
  ignore: (path) => path !== "" && path !== "/package.json" && path !== "/out" && !path.startsWith("/out/"),
});
console.log(`paket: ${paths.join(", ")}`);

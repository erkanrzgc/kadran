/**
 * Uygulamayla dağıtılan her üçüncü taraf parçasının lisansı izinli
 * listede. Çalışma zamanı paketleri (package-lock'ta dev olmayanlar)
 * arayüze gömülüyor; kopyalanan ikonlar ve yazı tipi de öyle. Kadran
 * Apache-2.0; izinli lisanslar onunla birlikte dağıtılabilenler.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const root = join(here, "..");
export const ALLOWED_LICENSES = new Set(["MIT", "ISC", "BSD-2-Clause", "BSD-3-Clause", "Apache-2.0", "0BSD", "OFL-1.1"]);

interface LockPackage {
  version?: string;
  license?: string;
  dev?: boolean;
  devOptional?: boolean;
}

test("çalışma zamanı paketlerinin lisansı izinli", async () => {
  const lock = JSON.parse(await readFile(join(root, "package-lock.json"), "utf8")) as { packages: Record<string, LockPackage> };
  const runtime = Object.entries(lock.packages).filter(([path, p]) => path !== "" && !p.dev && !p.devOptional);
  assert.ok(runtime.length >= 3, `yalnız ${runtime.length} çalışma zamanı paketi — package-lock biçimi değişmiş olabilir`);
  const bad = runtime.filter(([, p]) => !ALLOWED_LICENSES.has(p.license ?? "")).map(([path, p]) => `${path}: ${p.license}`);
  assert.deepEqual(bad, []);
});

test("lisans dosyası: başka platformun yerel paketi kurulu olmasa da betik durmaz, kurulu olan listelenir", async () => {
  const notices = await readFile(join(root, "scripts", "notices.mjs"), "utf8");
  assert.match(notices, /p\.optional && !existsSync\(join\(desktop, path\)\)/);
});

test("gömülü yazı tipinin lisansı yerinde", async () => {
  const ofl = await readFile(join(root, "src", "renderer", "fonts", "OFL.txt"), "utf8");
  assert.match(ofl, /SIL OPEN FONT LICENSE/i);
  assert.match(ofl, /Geist/);
});

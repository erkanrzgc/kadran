/**
 * Uçtan uca test: Electron'u duman kipinde, yolculukla ve sahte sidecar'la
 * açar; çıkış kodunu aynen döner.
 *
 * KADRAN_SIDECAR_COMMAND boşluktan bölünüyor (spawn-sidecar.ts); bu yüzden
 * sahte sidecar'ın yolunda boşluk olmamalı. "node" PATH'ten bulunur.
 */

import { spawn } from "node:child_process";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

import electron from "electron";

const here = dirname(fileURLToPath(import.meta.url));
const desktop = resolve(here, "..");
const fake = join(desktop, "test", "fixtures", "fake-sidecar.mjs");
if (/\s/.test(fake)) {
  console.error(`sahte sidecar yolunda boşluk var, KADRAN_SIDECAR_COMMAND bölünür: ${fake}`);
  process.exit(2);
}

const child = spawn(electron, ["."], {
  cwd: desktop,
  stdio: "inherit",
  env: {
    ...process.env,
    KADRAN_SMOKE_TEST: "1",
    KADRAN_SMOKE_JOURNEY: "1",
    KADRAN_SIDECAR_COMMAND: `node ${fake}`,
  },
});
child.on("exit", (code) => process.exit(code ?? 1));

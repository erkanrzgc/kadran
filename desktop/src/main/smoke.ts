/**
 * Duman testi kipi (KADRAN_SMOKE_TEST=1).
 *
 * Pencereyi gösterme; yüklendiğini, köprünün yerinde olduğunu, Node'un
 * sızmadığını ve konsola hata düşmediğini doğrula; çık. Bir GUI'nin
 * "açılıyor mu" sorusu ancak çalıştırılarak yanıtlanır. Görev 17 bu
 * dosyaya sahte sidecar'a karşı bir kullanıcı yolculuğu ekler.
 *
 * Çıkış kodu 0 = geçti, 1 = kaldı.
 */

import { app, type BrowserWindow } from "electron";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const SMOKE_TEST = process.env.KADRAN_SMOKE_TEST === "1";

export const EXPECTED_BRIDGE = [
  "auditList",
  "auditVerify",
  "copyText",
  "loadAppearance",
  "loadProfiles",
  "platform",
  "saveAppearance",
  "saveProfiles",
  "status",
  "version",
  "windowControl",
];

const ROOT_POLL_MS = 100;
const ROOT_TIMEOUT_MS = 5_000;

/**
 * prepareSmokeUserData, testi kullanıcının gerçek verisinden ayırır:
 * geçici bir kullanıcı verisi dizini, iki sahte profil ve Solid mod
 * (CI'daki sanal ekranda saydamlık yok). `whenReady`'den ÖNCE çağrılmalı.
 */
export function prepareSmokeUserData(): string {
  const dir = mkdtempSync(join(tmpdir(), "kadran-smoke-"));
  writeFileSync(
    join(dir, "profiles.json"),
    JSON.stringify([
      { name: "fake", target: "fake@test" },
      { name: "down", target: "fake@down" },
    ]),
  );
  writeFileSync(join(dir, "appearance.json"), JSON.stringify({ mode: "solid" }));
  app.setPath("userData", dir);
  return dir;
}

export async function evaluate<T>(win: BrowserWindow, code: string): Promise<T> {
  return (await win.webContents.executeJavaScript(code, true)) as T;
}

/**
 * waitForRoot: React kökü bir IPC gidiş-dönüşünden SONRA çizilebilir
 * (main.tsx görünümü yükleyip öyle render edecek), bu yüzden tek bir
 * anlık yoklama yerine sınırlı süre yoklanır.
 */
async function waitForRoot(win: BrowserWindow): Promise<boolean> {
  const probe = "Boolean(document.querySelector('[data-testid=\"app\"]'))";
  const deadline = Date.now() + ROOT_TIMEOUT_MS;
  for (;;) {
    if (await evaluate<boolean>(win, probe)) return true;
    if (Date.now() >= deadline) return false;
    await new Promise((resolve) => setTimeout(resolve, ROOT_POLL_MS));
  }
}

export async function runSmokeTest(win: BrowserWindow): Promise<void> {
  const problems: string[] = [];

  win.webContents.on("console-message", (_event, level, message) => {
    // level 3 = error. CSP ihlalleri de buraya düşer.
    if (level >= 3) problems.push(`console error: ${message}`);
  });
  win.webContents.on("did-fail-load", (_event, code, description) => {
    problems.push(`page failed to load (${code}): ${description}`);
  });
  win.webContents.on("preload-error", (_event, path, error) => {
    problems.push(`preload error (${path}): ${error.message}`);
  });

  await new Promise<void>((done) => {
    win.webContents.once("did-finish-load", () => done());
    setTimeout(() => {
      problems.push("page did not load in 20 s");
      done();
    }, 20_000);
  });

  try {
    // Renderer'ın gördüğü yüzeyi renderer'a sorarak doğruluyoruz: ana
    // süreçten bakmak preload'un gerçekten çalıştığını kanıtlamazdı.
    const surface = await evaluate<string[]>(win, "Object.keys(window.kadran ?? {}).sort()");
    if (JSON.stringify(surface) !== JSON.stringify(EXPECTED_BRIDGE)) {
      problems.push(`bridge surface differs: ${JSON.stringify(surface)} != ${JSON.stringify(EXPECTED_BRIDGE)}`);
    }
    if (!(await waitForRoot(win))) problems.push("React root did not render");

    // Renderer Node'a ERİŞEMEMELİ; erişebiliyorsa ayrıcalık ayrımı
    // istemci tarafından delinmiş olur.
    const leaked = await evaluate<string>(win, "[typeof require, typeof process, typeof module].join(',')");
    if (leaked !== "undefined,undefined,undefined") problems.push(`Node leaked into the renderer: ${leaked}`);
  } catch (err) {
    problems.push(`could not query the bridge: ${(err as Error).message}`);
  }

  if (problems.length === 0) {
    console.log("duman testi GEÇTİ: pencere yüklendi, köprü yerinde, Node sızmıyor");
    app.exit(0);
    return;
  }
  for (const problem of problems) console.error("duman testi:", problem);
  app.exit(1);
}

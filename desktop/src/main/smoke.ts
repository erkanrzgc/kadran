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
      { name: "badseq", target: "fake@badseq" },
      { name: "flaky", target: "fake@flaky" },
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

const JOURNEY = process.env.KADRAN_SMOKE_JOURNEY === "1";

const q = (selector: string): string => `document.querySelector(${JSON.stringify(selector)})`;

async function waitFor(win: BrowserWindow, expr: string, label: string, timeoutMs = 15_000): Promise<void> {
  const until = Date.now() + timeoutMs;
  while (Date.now() < until) {
    if (await evaluate<boolean>(win, `Boolean(${expr})`)) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`timed out waiting for ${label}`);
}

async function click(win: BrowserWindow, selector: string): Promise<void> {
  const ok = await evaluate<boolean>(win, `(() => { const el = ${q(selector)}; if (!el) return false; el.click(); return true; })()`);
  if (!ok) throw new Error(`not found: ${selector}`);
}

async function text(win: BrowserWindow, selector: string): Promise<string> {
  return evaluate<string>(win, `${q(selector)}?.textContent ?? ""`);
}

async function rowSeqs(win: BrowserWindow, tableId: string): Promise<string[]> {
  return evaluate<string[]>(win, `[...document.querySelectorAll('[data-testid="${tableId}"] [data-testid="audit-row"]')].map((r) => r.dataset.seq)`);
}

function expect(cond: boolean, message: string): void {
  if (!cond) throw new Error(message);
}

/**
 * journey, sahte sidecar'a (test/fixtures/fake-sidecar.mjs) karşı bir
 * kullanıcının yaptıklarını yapar ve ekranı ölçer. Konsol hataları (CSP
 * ihlalleri dahil) runSmokeTest'in dinleyicisinde toplanır; yolculuk üç
 * ekranı da gezer ve ikon animasyonlarını tetikler ki bir ihlal gizli
 * kalmasın.
 */
async function journey(win: BrowserWindow): Promise<void> {
  // Yazı tipi izni: liste boş dönerse izin işleyicisi bozuk demektir.
  const fonts = await evaluate<number>(win, "window.queryLocalFonts ? queryLocalFonts().then((l) => l.length) : -1");
  expect(fonts > 0, `font list is empty or missing (${fonts}); the local-fonts permission may be denied`);

  await click(win, '[data-testid="server-fake@test"]');
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Attention required"`, "health pill");
  await waitFor(win, `${q('[data-testid="chain-executor"] .badge')}?.textContent === "UNVERIFIABLE"`, "executor chain");

  const daemonBadge = await evaluate<string>(win, `${q('[data-testid="chain-daemon"] .badge')}.className`);
  const execBadge = await evaluate<string>(win, `${q('[data-testid="chain-executor"] .badge')}.className`);
  expect((await text(win, '[data-testid="chain-daemon"] .badge')) === "VALID", "daemon chain is not VALID");
  expect(daemonBadge.includes("is-ok") && execBadge.includes("is-warn"), `chain tones wrong: ${daemonBadge} / ${execBadge}`);

  // İnceleme odağı 1: "son kayıtlar" en YENİYİ göstermeli.
  await waitFor(win, `document.querySelectorAll('[data-testid="audit-preview"] [data-testid="audit-row"]').length === 10`, "audit preview");
  const preview = await rowSeqs(win, "audit-preview");
  expect(preview[0] === "1284" && preview[9] === "1275", `preview shows ${preview.join(",")}`);

  await click(win, '[data-testid="nav-audit"]');
  await waitFor(win, `document.querySelectorAll('[data-testid="audit-full"] [data-testid="audit-row"]').length === 50`, "audit page");
  let full = await rowSeqs(win, "audit-full");
  expect(full[0] === "1284" && full[49] === "1235", `audit page shows ${full[0]}…${full[49]}`);
  await click(win, '[data-testid="load-older"]');
  await waitFor(win, `document.querySelectorAll('[data-testid="audit-full"] [data-testid="audit-row"]').length === 100`, "older page");
  full = await rowSeqs(win, "audit-full");
  expect(full[99] === "1185", `older page ends at ${full[99]}`);
  expect(Boolean(await evaluate<boolean>(win, `Boolean(${q('[data-testid="outcome-bar"] .outcome-seg')})`)), "outcome bar is empty");

  // Bozuk latest_seq HATA olmalı, "zincirde kayıt yok" değil.
  await click(win, '[data-testid="nav-status"]');
  await click(win, '[data-testid="server-fake@badseq"]');
  await waitFor(
    win,
    `${q('[data-testid="audit-preview"] [data-testid="audit-placeholder"]')}?.textContent === "Could not load audit records."`,
    "audit load error for a malformed latest_seq",
  );

  // Bağlantı koptuktan sonra yenileme: ekran bayat işaretlenmeli.
  await click(win, '[data-testid="server-fake@flaky"]');
  await waitFor(win, q('[data-testid="screen-status"]'), "status screen for fake@flaky");
  await click(win, '[data-testid="refresh"]');
  await waitFor(win, `${q(".stage-head .eyebrow")}?.textContent === "Connection lost"`, "stale eyebrow");
  expect((await text(win, ".stage-head")).includes("out of date"), "stale header does not say the values may be out of date");
  expect((await text(win, '[data-testid="health"]')) !== "Healthy", "health pill still says Healthy while stale");

  await click(win, '[data-testid="nav-settings"]');
  await waitFor(win, q('[data-testid="screen-settings"]'), "settings screen");
  // İkon animasyonu: Motion'ın yazdığı her şey CSP'ye takılmamalı.
  await evaluate(win, `${q('[data-testid="nav-status"]')}.dispatchEvent(new MouseEvent("mouseover", { bubbles: true }))`);
  await new Promise((r) => setTimeout(r, 800));

  await click(win, '[data-testid="lang-tr"]');
  await waitFor(win, `${q('[data-testid="nav-settings"]')}?.textContent === "Ayarlar"`, "Turkish UI");

  // İnceleme odağı 5: Türkçe arayüzde İngilizce sunucu hatası tam ve düz metin.
  // Önceki hata bildirimleri ekranda kalmış olabilir; TÜM bildirimler aranır.
  await click(win, '[data-testid="server-fake@down"]');
  const downToast = `[...document.querySelectorAll('[data-testid="toast"] .toast-text')].find((el) => el.textContent.includes("Connection refused"))`;
  await waitFor(win, downToast, "error toast");
  const toast = await evaluate<string>(win, `${downToast}.textContent`);
  expect(toast.startsWith("Bağlanılamadı: could not connect to fake@down"), `toast text: ${toast}`);
  expect(toast.includes("authorized_keys"), "toast text was cut");
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
    const timer = setTimeout(() => {
      problems.push("page did not load in 20 s");
      done();
    }, 20_000);
    win.webContents.once("did-finish-load", () => {
      clearTimeout(timer);
      done();
    });
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

    if (JOURNEY) {
      try {
        await journey(win);
      } catch (err) {
        problems.push(`journey: ${(err as Error).message}`);
      }
    }
  } catch (err) {
    problems.push(`could not query the bridge: ${(err as Error).message}`);
  }

  if (problems.length === 0) {
    console.log(
      JOURNEY
        ? "duman testi GEÇTİ: yolculuk tamam (sahte sidecar)"
        : "duman testi GEÇTİ: pencere yüklendi, köprü yerinde, Node sızmıyor",
    );
    app.exit(0);
    return;
  }
  for (const problem of problems) console.error("duman testi:", problem);
  app.exit(1);
}

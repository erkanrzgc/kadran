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
import { mkdirSync, mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

export const SMOKE_TEST = process.env.KADRAN_SMOKE_TEST === "1";

export const EXPECTED_BRIDGE = [
  "alarmsList",
  "appCreate",
  "appDelete",
  "appDeploy",
  "appGet",
  "appLogs",
  "appRollback",
  "appUpdate",
  "appsList",
  "auditList",
  "auditVerify",
  "backupCreate",
  "backupsList",
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
      { name: "verifyfail", target: "fake@verifyfail" },
      { name: "slow", target: "fake@slow" },
    ]),
  );
  writeFileSync(join(dir, "appearance.json"), JSON.stringify({ mode: process.env.KADRAN_SMOKE_MODE === "clear" ? "clear" : "solid" }));
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

async function fill(win: BrowserWindow, selector: string, value: string): Promise<void> {
  const ok = await evaluate<boolean>(win, `(() => { const el = ${q(selector)}; if (!el) return false; const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype; Object.getOwnPropertyDescriptor(proto, "value").set.call(el, ${JSON.stringify(value)}); el.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`);
  if (!ok) throw new Error(`not found: ${selector}`);
}

async function noHorizontalOverflow(win: BrowserWindow, label: string): Promise<void> {
  const offenders = await evaluate<string[]>(win, `(() => { const nodes = [...document.querySelectorAll('.app, .stage, .rail, .panel, .table-wrap, .settings, .resource-card, .operation-dialog')]; return nodes.filter((el) => el.clientWidth > 0 && el.scrollWidth > el.clientWidth + 1).map((el) => el.className + ':' + el.scrollWidth + '>' + el.clientWidth); })()`);
  expect(offenders.length === 0, `${label} horizontal overflow: ${offenders.join(", ")}`);
}

async function screenshot(win: BrowserWindow, name: string): Promise<void> {
  // A DOM update can precede its compositor frame, especially in hidden windows.
  await evaluate(win, "new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true))))");
  const dir = join(app.getPath("userData"), "evidence");
  mkdirSync(dir, { recursive: true });
  const capture = await win.capturePage(undefined, { stayHidden: true, stayAwake: true });
  expect(!capture.isEmpty(), `empty screenshot: ${name}`);
  const path = join(dir, `${name}.png`);
  writeFileSync(path, capture.toPNG());
  if (process.env.KADRAN_SMOKE_MODE === "clear") {
    const bitmap = capture.toBitmap();
    let translucent = 0;
    let visible = 0;
    for (let index = 3; index < bitmap.length; index += 4) { if (bitmap[index]! > 0) visible++; if (bitmap[index]! > 0 && bitmap[index]! < 255) translucent++; }
    expect(visible > 1000 && translucent > 1000, `Clear mode does not retain visible translucent pixels (${visible}, ${translucent})`);
    console.log(`Clear alpha evidence: ${translucent} translucent pixels`);
  }
  console.log(`UI evidence: ${path}`);
}

async function managementJourney(win: BrowserWindow): Promise<void> {
  await click(win, '[data-testid="nav-apps"]');
  await waitFor(win, q('[data-testid="app-card-hello"]'), "application cards");
  await waitFor(win, `${q('[data-testid="brand-logo"]')}?.naturalWidth === 1254`, "original logo image");
  const glass = await evaluate<string>(win, `getComputedStyle(${q('.resource-card')}).backdropFilter`);
  expect(glass.includes("blur"), `glass surfaces have no real backdrop blur (${glass}); ${await evaluate<string>(win, "JSON.stringify({spaced:CSS.supports('backdrop-filter','blur(24px) saturate(135%)'),minified:CSS.supports('backdrop-filter','blur(24px)saturate(135%)'),rules:[...document.styleSheets].flatMap(s=>[...s.cssRules]).filter(r=>r.selectorText==='.glass').map(r=>r.cssText)})")}`);
  await screenshot(win, "apps-dark-1180x780");
  await click(win, '[data-testid="app-card-hello"]');
  await waitFor(win, `${q('[data-testid="app-active-release"]')}?.textContent === "r2"`, "active release");
  await click(win, '[data-testid="app-tab-configuration"]');
  expect(await evaluate<boolean>(win, `${q('[data-testid="app-delete"]')}.disabled`), "live app deletion is enabled");

  await click(win, '[data-testid="app-deploy"]');
  expect(await evaluate<boolean>(win, `document.activeElement === ${q('[data-testid="operation-confirmation"]')}`), "confirmation dialog did not focus its input");
  expect(await evaluate<boolean>(win, `${q('[data-testid="operation-submit"]')}.disabled`), "deploy accepted without typed confirmation");
  await fill(win, '[data-testid="operation-confirmation"]', "hello");
  await fill(win, '[data-testid="deploy-ref"]', "fail");
  await click(win, '[data-testid="operation-submit"]');
  expect(await evaluate<boolean>(win, `${q('[data-testid="operation-submit"]')}.disabled`), "duplicate deploy remains enabled");
  await waitFor(win, `${q('[data-testid="operation-error"]')}?.textContent.includes("health gate failed")`, "deployment failure");
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="operation-result"]')})`)), "failed deploy reported success");
  await fill(win, '[data-testid="deploy-ref"]', "main");
  await click(win, '[data-testid="operation-submit"]');
  await waitFor(win, `${q('[data-testid="operation-result"]')}?.textContent.includes("Deployment succeeded: r3")`, "deployment success");
  await waitFor(win, `${q('[data-testid="app-active-release"]')}?.textContent === "r3"`, "new active release");

  await click(win, '[data-testid="app-rollback"]');
  await fill(win, '[data-testid="operation-confirmation"]', "hello");
  await click(win, '[data-testid="operation-submit"]');
  await waitFor(win, `${q('[data-testid="operation-result"]')}?.textContent.includes("from r3 to r2")`, "rollback result");
  await waitFor(win, `${q('[data-testid="app-active-release"]')}?.textContent === "r2"`, "rollback active release");

  await click(win, '[data-testid="app-tab-logs"]');
  await waitFor(win, `${q('[data-testid="app-logs"]')}?.textContent.includes("Merhaba")`, "UTF-8 logs");
  expect(!(await evaluate<boolean>(win, `Boolean(document.querySelector('#unsafe-log'))`)), "log output was interpreted as HTML");
  await click(win, '[data-testid="app-tab-configuration"]');
  expect((await text(win, '[data-testid="app-existing-env"]')).includes("MODE"), "existing environment names are missing");
  expect(await evaluate<boolean>(win, `${q('[data-testid="app-envText"]')}.value === ""`), "redacted environment values were added as replacements");
  await fill(win, '[data-testid="app-domain"]', "changed.example.com");
  await click(win, '[data-testid="app-save"]');
  await waitFor(win, `${q('[data-testid="operation-result"]')}?.textContent.includes("Configuration saved")`, "domain-only configuration update");
  await waitFor(win, `${q('[data-testid="app-domain"]')}?.value === "changed.example.com"`, "domain-only refreshed configuration");
  expect((await text(win, '[data-testid="app-existing-env"]')).includes("OLD"), "domain update removed an existing environment name");
  await fill(win, '[data-testid="app-envText"]', "MODE=new\nEMPTY=");
  await fill(win, '[data-testid="app-envRemoveText"]', "OLD");
  await click(win, '[data-testid="app-save"]');
  await waitFor(win, `${q('[data-testid="operation-result"]')}?.textContent.includes("Configuration saved")`, "configuration update");
  await waitFor(win, `${q('[data-testid="app-domain"]')}?.value === "changed.example.com"`, "updated configuration");
  expect(!(await text(win, '[data-testid="app-existing-env"]')).includes("OLD"), "explicit environment removal was not applied");

  win.setSize(760, 520);
  await waitFor(win, "window.innerWidth <= 760", "minimum window size");
  await noHorizontalOverflow(win, "configuration at 760x520");
  await evaluate(win, `${q('[data-testid="app-form"]')}.scrollIntoView({block:'start'})`);
  await screenshot(win, "configuration-dark-760x520");
  await click(win, '[data-testid="app-tab-deployments"]');
  await noHorizontalOverflow(win, "releases at 760x520");
  await click(win, '[data-testid="release-r2"] summary');
  await noHorizontalOverflow(win, "release detail at 760x520");
  await evaluate(win, `${q('[data-testid="release-r2"]')}.scrollIntoView({block:'start'})`);
  await screenshot(win, "releases-dark-760x520");

  await click(win, '[data-testid="apps-back"]');
  await waitFor(win, q('[data-testid="app-card-hello"]'), "application list after back");
  await click(win, '[data-testid="app-new"]');
  await fill(win, '[data-testid="app-appId"]', "INVALID");
  await click(win, '[data-testid="app-save"]');
  await waitFor(win, q('[data-testid="form-validation"]'), "application validation");
  await fill(win, '[data-testid="app-appId"]', "demo");
  await fill(win, '[data-testid="app-gitOwner"]', "team");
  await fill(win, '[data-testid="app-gitRepo"]', "site");
  await click(win, '[data-testid="app-save"]');
  await waitFor(win, `${q('[data-testid="app-active-release"]')}?.textContent === "No active deployment"`, "created app has no deployment");
  await click(win, '[data-testid="app-tab-configuration"]');
  await click(win, '[data-testid="app-delete"]');
  await fill(win, '[data-testid="operation-confirmation"]', "demo");
  await noHorizontalOverflow(win, "confirmation at 760x520");
  await click(win, '[data-testid="operation-submit"]');
  await waitFor(win, q('[data-testid="app-card-hello"]'), "application list after deletion");
  await waitFor(win, `${q('[data-testid="app-deleted"]')}?.textContent.includes("demo")`, "confirmed deletion result");
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="app-card-demo"]')})`)), "deleted definition still exists");

  await click(win, '[data-testid="nav-alarms"]');
  await waitFor(win, q('[data-testid="alarm-row"]'), "alarm list");
  await noHorizontalOverflow(win, "alarms at 760x520");
  await click(win, '[data-testid="nav-backups"]');
  await waitFor(win, q('[data-testid="backup-row"]'), "backup list");
  expect((await text(win, '[data-testid="backups-scope"]')).includes("Application volumes"), "backup scope is missing");
  await click(win, '[data-testid="backup-create"]');
  await waitFor(win, `${q('[data-testid="backup-error"]')}?.textContent.includes("disk full")`, "backup failure");
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="backup-result"]')})`)), "failed backup reported success");
  await click(win, '[data-testid="backup-create"]');
  await waitFor(win, q('[data-testid="backup-result"]'), "backup success");
  await waitFor(win, `document.querySelectorAll('[data-testid="backup-row"]').length === 2`, "backup refreshed list");
  await noHorizontalOverflow(win, "backups at 760x520");

  await click(win, '[data-testid="server-fake@slow"]');
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Healthy"`, "slow server connected");
  await click(win, '[data-testid="nav-apps"]');
  await waitFor(win, q('[data-testid="screen-apps"]'), "slow application read started");
  await click(win, '[data-testid="server-fake@test"]');
  await waitFor(win, q('[data-testid="app-card-hello"]'), "new server application list");
  await new Promise((r) => setTimeout(r, 1900));
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="app-card-old-only"]')})`)), "old-server response overwrote new application list");
  await noHorizontalOverflow(win, "apps at 760x520");

  await click(win, '[data-testid="nav-status"]');
  await click(win, '[data-testid="server-fake@verifyfail"]');
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Healthy"`, "initial successful verification");
  const verifiedAt = await text(win, '[data-testid="verification-time"]');
  expect(verifiedAt.includes("Last verified:"), "last verification time missing");
  await click(win, '[data-testid="verify"]');
  await waitFor(win, `${q('[data-testid="verification-time"]')}?.textContent.includes("out of date")`, "stale verification marker");
  expect((await text(win, '[data-testid="health"]')) === "Attention required", "failed verification retained healthy status");
  expect(!(await evaluate<boolean>(win, `Boolean(document.querySelector('.chains .badge.is-ok'))`)), "failed verification retained current green chain");
  await noHorizontalOverflow(win, "status at 760x520");
  await click(win, '[data-testid="nav-audit"]');
  await waitFor(win, q('[data-testid="audit-detail-1284"]'), "minimum audit");
  await click(win, '[data-testid="audit-detail-1284"]');
  await noHorizontalOverflow(win, "audit detail at 760x520");
  await screenshot(win, "audit-dark-760x520");
  await click(win, '[data-testid="nav-settings"]');
  await noHorizontalOverflow(win, "settings at 760x520");
  await screenshot(win, "settings-dark-760x520");
  await click(win, '[data-testid="tone-light"]');
  await waitFor(win, "document.documentElement.dataset.tone === 'light'", "light appearance");
  await click(win, '[data-testid="server-fake@test"]');
  await click(win, '[data-testid="nav-apps"]');
  await waitFor(win, q('[data-testid="app-card-hello"]'), "light application list");
  await noHorizontalOverflow(win, "light apps at 760x520");
  await screenshot(win, "apps-light-760x520");
  await click(win, '[data-testid="nav-settings"]');
  await click(win, '[data-testid="tone-dark"]');
  await click(win, '[data-testid="nav-status"]');
  win.setSize(1180, 780);
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
  await click(win, '[data-testid="audit-detail-1284"]');
  await waitFor(win, q('[data-testid="audit-detail-row"]'), "audit record detail");

  await managementJourney(win);

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
  // fake@flaky sağlıklı başlar; bayatlık "Healthy"i düşürmeli (yalnız "Healthy değil" boş bir iddia olurdu).
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Healthy"`, "healthy pill for fake@flaky");
  await click(win, '[data-testid="refresh"]');
  await waitFor(win, `${q(".stage-head .eyebrow")}?.textContent === "Connection lost"`, "stale eyebrow");
  expect((await text(win, ".stage-head")).includes("out of date"), "stale header does not say the values may be out of date");
  const pill = await text(win, '[data-testid="health"]');
  expect(pill === "Attention required", `health pill while stale: ${pill}`);

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
  win.webContents.setBackgroundThrottling(false);

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

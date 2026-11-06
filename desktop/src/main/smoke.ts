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
import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

import { loadWindowAccent } from "./window-accent.ts";

export const SMOKE_TEST = process.env.KADRAN_SMOKE_TEST === "1";

export const EXPECTED_BRIDGE = [
  "addServer",
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
  "listServers",
  "loadAppearance",
  "platform",
  "removeServer",
  "saveAppearance",
  "serversImportStatus",
  "status",
  "updateServer",
  "version",
  "windowControl",
];

const ROOT_POLL_MS = 100;
const ROOT_TIMEOUT_MS = 5_000;

/**
 * prepareSmokeUserData, testi kullanıcının gerçek verisinden ayırır:
 * geçici bir kullanıcı verisi dizini ve sahte profiller. `whenReady`'den ÖNCE çağrılmalı.
 */
/** Çok uzun ad ve hedef: kartta taşma olmamalı. */
const LONG_NAME = "uzun-" + "ad".repeat(60);
const LONG_TARGET = "fake@" + "uzun-hedef.".repeat(11) + "example";

export function prepareSmokeUserData(): string {
  const dir = mkdtempSync(join(tmpdir(), "kadran-smoke-"));
  writeFileSync(
    join(dir, "profiles.json"),
    JSON.stringify([
      { name: "fake", target: "fake@test", provider: "hetzner" },
      { name: "down", target: "fake@down", provider: "hetzner" },
      { name: "badseq", target: "fake@badseq", provider: "google-cloud" },
      { name: "totp", target: "fake@totp", provider: "hostinger" },
      { name: "noalarms", target: "fake@noalarms" },
      { name: "root", target: "fake@root" },
      { name: "flaky", target: "fake@flaky" },
      { name: "flaky2", target: "fake@flaky2" },
      { name: "verifyfail", target: "fake@verifyfail" },
      { name: "slow", target: "fake@slow" },
      { name: LONG_NAME, target: LONG_TARGET, provider: "kendi-sunucum" },
    ]),
  );
  // Kaldırılan cam teması kayıtlı: açılışta açığa dönmeli (K-163).
  writeFileSync(join(dir, "appearance.json"), JSON.stringify({ version: 2, language: "en", theme: "glass" }));
  // Ortak sunucu listesi (K-162): sahte yan süreç bu dosyayı kullanır; yukarıdaki
  // profiles.json açılışta ona taşınır. Kullanıcının gerçek servers.json'una dokunulmaz.
  process.env.KADRAN_FAKE_SERVERS_FILE = join(dir, "fake-servers.json");
  app.setPath("userData", dir);
  return dir;
}

/** CLI'ın ortak listeye yazmasının taklidi: sahte yan sürecin dosyasına yazar. */
function writeSharedServers(change: (servers: unknown[]) => unknown[]): void {
  const file = process.env.KADRAN_FAKE_SERVERS_FILE;
  if (!file) throw new Error("KADRAN_FAKE_SERVERS_FILE is not set");
  const body = JSON.parse(readFileSync(file, "utf8")) as { version: number; servers: unknown[] };
  writeFileSync(file, JSON.stringify({ ...body, servers: change(body.servers) }));
}

const EVALUATE_TIMEOUT_MS = 30_000;

/** Sınırlı: takılan arayüz CI'ı sonsuza dek asmaz, neyin takıldığını söyleyerek düşer. */
export async function evaluate<T>(win: BrowserWindow, code: string): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  const timeout = new Promise<never>((_, reject) => {
    timer = setTimeout(() => reject(new Error(`renderer did not answer: ${code.slice(0, 120)}`)), EVALUATE_TIMEOUT_MS);
  });
  try {
    return (await Promise.race([win.webContents.executeJavaScript(code, true), timeout])) as T;
  } finally {
    clearTimeout(timer);
  }
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

/**
 * Kullanıcı gibi tıklar: öğe görünür olmalı ve tıklama noktasında üstte o olmalı.
 * el.click() tek başına display:none düğmeye de, bildirimin altındakine de basardı.
 */
async function click(win: BrowserWindow, selector: string): Promise<void> {
  const result = await evaluate<string>(win, `(() => {
    const el = ${q(selector)};
    if (!el) return "not found";
    el.scrollIntoView({ block: "center", inline: "center" });
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) return "not visible";
    const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    if (!top || !(el === top || el.contains(top) || top.contains(el))) return "covered by " + (top ? top.className || top.tagName : "nothing");
    el.click();
    return "";
  })()`);
  if (result) throw new Error(`${result}: ${selector}`);
}

async function text(win: BrowserWindow, selector: string): Promise<string> {
  return evaluate<string>(win, `${q(selector)}?.textContent ?? ""`);
}

async function fill(win: BrowserWindow, selector: string, value: string): Promise<void> {
  const ok = await evaluate<boolean>(win, `(() => { const el = ${q(selector)}; if (!el) return false; const proto = el instanceof HTMLTextAreaElement ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype; Object.getOwnPropertyDescriptor(proto, "value").set.call(el, ${JSON.stringify(value)}); el.dispatchEvent(new Event("input", { bubbles: true })); return true; })()`);
  if (!ok) throw new Error(`not found: ${selector}`);
}

async function noHorizontalOverflow(win: BrowserWindow, label: string): Promise<void> {
  const offenders = await evaluate<string[]>(win, `(() => { const nodes = [...document.querySelectorAll('.app, .main, .stage, .rail, .panel, .table-wrap, .settings, .resource-card, .kart, .operation-dialog')]; return nodes.filter((el) => el.clientWidth > 0 && el.scrollWidth > el.clientWidth + 1).map((el) => el.className + ':' + el.scrollWidth + '>' + el.clientWidth); })()`);
  expect(offenders.length === 0, `${label} horizontal overflow: ${offenders.join(", ")}`);
}

/** İki çizim karesi bekler: boyut izleyicileri ve rAF işleri o karelerde çalışır. */
async function nextFrames(win: BrowserWindow): Promise<void> {
  await evaluate(win, "new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve(true))))");
}

async function screenshot(win: BrowserWindow, name: string): Promise<void> {
  // A DOM update can precede its compositor frame, especially in hidden windows.
  await nextFrames(win);
  const dir = join(app.getPath("userData"), "evidence");
  mkdirSync(dir, { recursive: true });
  const capture = await win.capturePage(undefined, { stayHidden: true, stayAwake: true });
  expect(!capture.isEmpty(), `empty screenshot: ${name}`);
  const path = join(dir, `${name}.png`);
  writeFileSync(path, capture.toPNG());
  console.log(`UI evidence: ${path}`);
}

/**
 * Seçili satırın camı tek bir kayan gösterge (K-163, kullanıcının seçtiği 7):
 * görünür, cam dolgulu ve seçili satırın tam üstünde durmalı.
 */
async function pillOnActive(win: BrowserWindow, scope: string): Promise<void> {
  const pill = q(`${scope} .nav-pill`);
  const active = q(`${scope} .nav-item.is-active`);
  await waitFor(
    win,
    `(() => {
      const p = ${pill}, a = ${active};
      if (!p || !a) return false;
      const r = p.getBoundingClientRect(), s = a.getBoundingClientRect();
      return getComputedStyle(p).opacity === "1" && getComputedStyle(p).backgroundImage.includes("linear-gradient")
        && Math.abs(r.top - s.top) <= 1 && Math.abs(r.left - s.left) <= 1 && Math.abs(r.width - s.width) <= 1 && Math.abs(r.height - s.height) <= 1;
    })()`,
    `${scope}: the glass pill does not sit on the selected row`,
  );
}

async function checkWordmark(win: BrowserWindow): Promise<void> {
  const mark = await evaluate<{ readable: boolean; weight: string; spacing: string; text: string; offCenter: number; offPanel: number }>(win, `(() => {
    const el = ${q('[data-testid="brand-logo"]')};
    const rail = ${q(".rail")};
    // En üstteki panelin üst kenarı: sunucu sayfasında dar düzende o panel
    // ikinci menüdür, içerik paneli onun altına iner.
    const stage = ${q(".main > .stage, .main > .page-host")};
    if (!(el instanceof HTMLElement) || !(rail instanceof HTMLElement) || !(stage instanceof HTMLElement)) return { readable: false, weight: '', spacing: '', text: '', offCenter: 0, offPanel: 0 };
    const rect = el.getBoundingClientRect();
    const style = getComputedStyle(el);
    const offCenter = Math.abs(rect.left + rect.width / 2 - (rail.getBoundingClientRect().left + rail.clientWidth / 2));
    const offPanel = Math.abs(rect.top - stage.getBoundingClientRect().top - 16);
    return { readable: rect.width >= 60 && rect.height >= 12, weight: style.fontWeight, spacing: style.letterSpacing, text: el.textContent, offCenter, offPanel };
  })()`);
  expect(mark.readable && mark.text === "KΛDRΛN", "wordmark is missing or too small");
  expect(mark.weight === "700" && parseFloat(mark.spacing) > 2.5, `wordmark lost its style: ${mark.weight} ${mark.spacing}`);
  expect(mark.offCenter <= 1, `wordmark is not centered in the sidebar: ${mark.offCenter}px off`);
  // KADRAN panelin üst kenarının 16 px altından başlar (kullanıcı, 6 Kasım).
  expect(mark.offPanel <= 2, `wordmark is not 16px below the panel top: ${mark.offPanel}px off`);
}

/**
 * Duman testinde yerel malzeme kapalı: cam çerçeve opak düşer. Kayıtlı eski cam
 * teması açığa döner; iki tema da ana sürece ulaşır, içerik iki temada da opak
 * panelde (K-163).
 */
async function themeJourney(win: BrowserWindow): Promise<void> {
  const root = "document.documentElement";
  const shell = `getComputedStyle(${q(".shell")}).backgroundColor`;
  const sheet = `getComputedStyle(${q(".stage")}).backgroundColor`;
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="theme-glass"]')})`)), "the removed glass theme is still offered");
  await waitFor(win, `${root}.dataset.theme === "light" && ${shell} === "rgb(237, 237, 240)" && ${sheet} === "rgb(255, 255, 255)"`, "light is the default: solid frame, white panel");
  await waitFor(win, `matchMedia("(prefers-color-scheme: light)").matches`, "native light theme");
  const ink = await evaluate<number>(win, `(() => {
    const ctx = new OffscreenCanvas(1, 1).getContext("2d", { willReadFrequently: true });
    ctx.fillStyle = getComputedStyle(document.body).color;
    ctx.fillRect(0, 0, 1, 1);
    return ctx.getImageData(0, 0, 1, 1).data[0];
  })()`);
  expect(ink < 60, `light theme text is not dark: ${ink}`);
  await screenshot(win, "settings-light-1280x820");

  await click(win, '[data-testid="theme-dark"]');
  await waitFor(win, `${root}.dataset.theme === "dark" && ${shell} === "rgb(10, 10, 10)" && ${sheet} === "rgb(20, 20, 20)"`, "dark: solid frame, dark panel");
  await waitFor(win, `matchMedia("(prefers-color-scheme: dark)").matches`, "native dark theme");

  await evaluate(win, `window.dispatchEvent(new Event("blur"))`);
  await waitFor(win, `${root}.dataset.focused === "false"`, "window blur is tracked");
  await evaluate(win, `window.dispatchEvent(new Event("focus"))`);
  await waitFor(win, `${root}.dataset.focused === "true"`, "window focus is tracked");
}

async function dismissToasts(win: BrowserWindow): Promise<void> {
  await evaluate(win, `document.querySelectorAll(".toast-close").forEach((b) => b.click())`);
}

/**
 * openServer: kullanıcı gibi Sunucular ekranındaki karta basar. Ekranda değilse
 * önce oraya döner; sekmeden değil karttan açmak kart testid'lerini de sınar.
 */
async function openServer(win: BrowserWindow, target: string): Promise<void> {
  const card = `[data-testid="server-${target}"]`;
  if (!(await evaluate<boolean>(win, `Boolean(${q(card)})`))) await click(win, '[data-testid="rail-servers"]');
  await waitFor(win, q(card), `card ${target}`);
  await click(win, card);
}

/** Windows'ta köprü gerçekten yüklenir ve çağrı kabul edilir; yüklenemeseydi cam sessizce griye düşerdi. */
async function checkWindowAccent(win: BrowserWindow): Promise<void> {
  if (process.platform !== "win32") return;
  const accent = await loadWindowAccent((message) => expect(false, message));
  expect(accent !== null, "Windows glass bridge did not load");
  expect(accent!.set(win, false, "dark"), "Windows refused the glass call");
}

/**
 * Düz üst çubuk: içerik altından başlar; filo ekranında yol ve tek sekme yok
 * (sayfa adı başlıkta), pencere düğmeleri sağ köşede.
 */
async function checkTopBar(win: BrowserWindow): Promise<void> {
  const box = await evaluate<{ bar: number; stage: number; right: number; buttons: number }>(win, `({ bar: ${q(".topbar")}.getBoundingClientRect().bottom, stage: ${q(".stage")}.getBoundingClientRect().top, right: ${q(".topbar")}.getBoundingClientRect().right, buttons: ${q(".win-btns")}.getBoundingClientRect().right })`);
  expect(box.stage >= box.bar, `content starts under the top bar: ${box.stage} < ${box.bar}`);
  expect(Math.abs(box.right - box.buttons) <= 1, `window buttons are not in the corner: ${box.buttons} vs ${box.right}`);
  expect(!(await evaluate<boolean>(win, `Boolean(${q(".topbar .crumbs")})`)), "fleet top bar repeats the page name");
  const tabs = await evaluate<number>(win, `document.querySelectorAll(".topbar .tab").length`);
  expect(tabs !== 1, "top bar shows a lone tab");
}

/** Sunucular: gerçek çağrılardan kartlar; okunamayan hiçbir şey "yok" sayılmaz. */
async function fleetJourney(win: BrowserWindow): Promise<void> {
  const card = (target: string) => q(`[data-testid="card-${target}"]`);
  await waitFor(win, q('[data-testid="screen-servers"]'), "servers screen is the home screen");
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="rail-dashboard"]')})`)), "the removed dashboard is back in the sidebar");
  await checkTopBar(win);
  await waitFor(win, `${card("fake@test")}?.dataset.state === "ready"`, "ready card");
  await waitFor(win, `${card("fake@down")}?.dataset.state === "unreachable"`, "unreachable card");
  await waitFor(win, `${card("fake@totp")}?.dataset.state === "totp"`, "TOTP card");
  expect((await text(win, '[data-testid="card-fake@totp"]')).includes("kadran login"), "TOTP card does not tell the real next step");
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="toast"]')})`)), "fleet polling raised a toast for an unreachable server");
  await waitFor(win, `${card("fake@root")}?.dataset.tone === "bad"`, "broken install is not green");
  await waitFor(win, `${card("fake@noalarms")}?.dataset.tone === "warn"`, "unreadable alarms are not healthy");
  expect((await text(win, '[data-testid="card-fake@noalarms"]')).includes("alarms unknown"), "card with unreadable alarms does not say so");
  const providers = await evaluate<string[]>(win, `[...document.querySelectorAll('[data-testid="server-groups"] > div')].map((s) => s.dataset.provider)`);
  expect(providers[0] === "hetzner" && providers[1] === "hetzner" && providers.at(-1) === "", `provider order: ${providers.join(",")}`);
  expect((await text(win, '[data-testid="card-fake@test"]')).includes("2 applications"), "card does not show the real application count");
  await pillOnActive(win, ".rail");
  await noHorizontalOverflow(win, "servers");
  await screenshot(win, "servers-1280x820");

  // Eski profil listesi açılışta ortak listeye taşındı (yukarıdaki kartlar onu
  // okudu); CLI'da eklenen sunucu pencere odağa gelince kart olarak çıkar,
  // silinen de kaybolur.
  writeSharedServers((servers) => [...servers, { name: "from-cli", target: "fake@cli" }]);
  await evaluate(win, `window.dispatchEvent(new Event("focus"))`);
  await waitFor(win, card("fake@cli"), "server added from the CLI appears on focus");
  writeSharedServers((servers) => servers.filter((s) => (s as { target: string }).target !== "fake@cli"));
  await evaluate(win, `window.dispatchEvent(new Event("focus"))`);
  await waitFor(win, `!${card("fake@cli")}`, "server removed from the CLI disappears on focus");

  await click(win, '[data-testid="rail-alarms"]');
  await waitFor(win, q('[data-testid="fleet-alarm-row"]'), "merged alarms");
  await pillOnActive(win, ".rail");
  await waitFor(win, `${q('[data-testid="fleet-unread"]')}?.textContent.includes("noalarms")`, "server with unreadable alarms named");
  const badge = await text(win, '[data-testid="rail-alarms"] .nav-count');
  expect(Number(badge) >= 1, `alarm badge is not a count: ${badge}`);
  await click(win, '[data-testid="rail-apps"]');
  await waitFor(win, q('[data-testid="fleet-app-row"]'), "merged applications");
  await click(win, '[data-testid="rail-audit"]');
  await waitFor(win, q('[data-testid="fleet-audit-row"]'), "merged audit");
  await waitFor(win, `${q('[data-testid="ops-failed"]')}?.textContent.includes("badseq")`, "unreadable server named");
  const wrapped = await evaluate<number>(win, `[...document.querySelectorAll('[data-testid="fleet-audit-row"] .pill')].filter((p) => p.getBoundingClientRect().height > 26).length`);
  expect(wrapped === 0, `status pill wrapped in ${wrapped} rows`);
  await click(win, '[data-testid="rail-servers"]');
  await waitFor(win, q('[data-testid="screen-servers"]'), "servers screen");

  // Kart eylemleri gizli başlar, klavye odağında görünür.
  const actions2 = q('[data-testid="card-edit-fake@verifyfail"]');
  const parent = `${actions2}.parentElement`;
  expect(await evaluate<string>(win, `getComputedStyle(${parent}).opacity`) === "0", "card actions are visible at rest");
  await evaluate(win, `${actions2}.focus()`);
  await waitFor(win, `getComputedStyle(${parent}).opacity === "1"`, "card actions on keyboard focus");

  // Sağlayıcı etiketi diske gider ve kart yerini bulur.
  await click(win, '[data-testid="card-edit-fake@verifyfail"]');
  await waitFor(win, q('[data-testid="server-dialog"]'), "server settings");
  await evaluate(win, `(() => { const el = ${q('[data-testid="server-provider"]')}; Object.getOwnPropertyDescriptor(HTMLSelectElement.prototype, "value").set.call(el, "aws"); el.dispatchEvent(new Event("change", { bubbles: true })); })()`);
  await click(win, '[data-testid="connect"]');
  await waitFor(win, `${q('[data-provider="aws"] [data-testid="card-fake@verifyfail"]')}`, "provider after edit");

  // Ulaşılamayan sunucu kaydedilmez; sebep kutunun içinde, bildirimde değil.
  await click(win, '[data-testid="server-add"]');
  await fill(win, '[data-testid="target-input"]', "fake@down");
  await click(win, '[data-testid="connect"]');
  await waitFor(win, `${q('[data-testid="server-error"]')}?.textContent.includes("Connection refused")`, "add failure shown in the dialog");
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="toast"]')})`)), "add failure went to a toast behind the dialog");
  await click(win, '[data-testid="server-cancel"]');

  // TOTP isteyen sunucu yerinde: kaydedilir, kart giriş ister.
  await click(win, '[data-testid="server-add"]');
  await fill(win, '[data-testid="target-input"]', "fake@totp-new");
  await click(win, '[data-testid="connect"]');
  await waitFor(win, `${card("fake@totp-new")}`, "TOTP server added");
  await waitFor(win, `${card("fake@totp-new")}?.dataset.state === "totp"`, "added TOTP server asks for sign-in");

  // Açık sunucuya dönüş eski değeri "Sağlıklı" bırakmaz: sunucu düştüyse sekme kapanır.
  await openServer(win, "fake@flaky2");
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Healthy"`, "flaky connected");
  expect(!(await evaluate<boolean>(win, `Boolean(${q(".topbar .tabs")})`)), "one open server shows a tab strip");
  await click(win, '[data-testid="rail-servers"]');
  await openServer(win, "fake@flaky2");
  await waitFor(win, `!${q('[data-testid="tab-fake@flaky2"]')} && ${q('[data-testid="screen-servers"]')}`, "reopened dead server left the old values");
  await waitFor(win, q('[data-testid="toast"]'), "reason for the closed tab");
  await dismissToasts(win);
}

async function managementJourney(win: BrowserWindow): Promise<void> {
  await click(win, '[data-testid="nav-apps"]');
  await waitFor(win, q('[data-testid="app-card-hello"]'), "application cards");
  await pillOnActive(win, ".subnav");
  await checkWordmark(win);
  const sheetFill = await evaluate<string>(win, `getComputedStyle(${q(".page-host > .stage")}).backgroundColor`);
  expect(sheetFill === "rgb(255, 255, 255)", `application cards are not on the opaque panel: ${sheetFill}`);
  await screenshot(win, "apps-1280x820");
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
  // Windows rounds a transparent window by one CSS pixel at fractional DPI.
  await waitFor(win, "Math.abs(window.innerWidth - 760) <= 1", "minimum window size");
  // Kenar çubuğunun göstergeleri boyut izleyicisiyle bir sonraki karede yeniden
  // ölçülür (K-163); gizli pencerede o kare genişlik değişiminden sonra gelir.
  await nextFrames(win);
  // Dar düzende ikinci menü panelin üstüne geçer; logo yine en üstteki panele
  // göre ölçülmeli. Windows CI'da pencere 1280 px açılmıyor: oradaki 50 px
  // fark tam dar düzenin farkıydı (ikinci menü 42 px + 8 px boşluk).
  await checkWordmark(win);
  await noHorizontalOverflow(win, "configuration at 760x520");
  await evaluate(win, `${q('[data-testid="app-form"]')}.scrollIntoView({block:'start'})`);
  await screenshot(win, "configuration-760x520");
  await click(win, '[data-testid="app-tab-deployments"]');
  await noHorizontalOverflow(win, "releases at 760x520");
  await click(win, '[data-testid="release-r2"] summary');
  await noHorizontalOverflow(win, "release detail at 760x520");
  await evaluate(win, `${q('[data-testid="release-r2"]')}.scrollIntoView({block:'start'})`);
  await screenshot(win, "releases-760x520");

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

  await openServer(win, "fake@slow");
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Healthy"`, "slow server connected");
  await click(win, '[data-testid="nav-apps"]');
  await waitFor(win, q('[data-testid="screen-apps"]'), "slow application read started");
  await openServer(win, "fake@test");
  await waitFor(win, q('[data-testid="app-card-hello"]'), "new server application list");
  await new Promise((r) => setTimeout(r, 1900));
  expect(!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="app-card-old-only"]')})`)), "old-server response overwrote new application list");
  await noHorizontalOverflow(win, "apps at 760x520");
  const tabVisible = await evaluate<boolean>(win, `(() => { const s = ${q(".tabs")}.getBoundingClientRect(); const a = ${q(".tab.is-on")}.getBoundingClientRect(); return a.left >= s.left - 1 && a.right <= s.right + 1; })()`);
  expect(tabVisible, "the open tab is scrolled out of view");
  await click(win, '[data-testid="rail-servers"]');
  await waitFor(win, q('[data-testid="screen-servers"]'), "servers at 760x520");
  await noHorizontalOverflow(win, "servers at 760x520");
  await checkTopBar(win);
  await openServer(win, "fake@test");
  await waitFor(win, q('[data-testid="app-card-hello"]'), "back to the application list");

  await openServer(win, "fake@verifyfail");
  await click(win, '[data-testid="nav-status"]');
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
  await screenshot(win, "audit-760x520");
  await click(win, '[data-testid="nav-settings"]');
  await noHorizontalOverflow(win, "settings at 760x520");
  await screenshot(win, "settings-760x520");
  win.setSize(1280, 820);
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

  await checkWindowAccent(win);
  await fleetJourney(win);
  await openServer(win, "fake@test");
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
  await openServer(win, "fake@badseq");
  await click(win, '[data-testid="nav-status"]');
  await waitFor(
    win,
    `${q('[data-testid="audit-preview"] [data-testid="audit-placeholder"]')}?.textContent === "Could not load audit records."`,
    "audit load error for a malformed latest_seq",
  );

  // Bağlantı koptuktan sonra yenileme: ekran bayat işaretlenmeli.
  await openServer(win, "fake@flaky");
  await waitFor(win, q('[data-testid="screen-status"]'), "status screen for fake@flaky");
  // fake@flaky sağlıklı başlar; bayatlık "Healthy"i düşürmeli (yalnız "Healthy değil" boş bir iddia olurdu).
  await waitFor(win, `${q('[data-testid="health"]')}?.textContent === "Healthy"`, "healthy pill for fake@flaky");
  await click(win, '[data-testid="refresh"]');
  await waitFor(win, `${q(".stage-head .eyebrow")}?.textContent === "Connection lost"`, "stale eyebrow");
  expect((await text(win, ".stage-head")).includes("out of date"), "stale header does not say the values may be out of date");
  const pill = await text(win, '[data-testid="health"]');
  expect(pill === "Attention required", `health pill while stale: ${pill}`);
  await dismissToasts(win);

  await click(win, '[data-testid="nav-settings"]');
  await waitFor(win, q('[data-testid="screen-settings"]'), "settings screen");
  await themeJourney(win);
  await click(win, '[data-testid="lang-tr"]');
  await waitFor(win, `${q('[data-testid="nav-settings"]')}?.textContent === "Ayarlar"`, "Turkish UI");

  // İnceleme odağı 5: Türkçe arayüzde İngilizce sunucu hatası tam ve düz metin.
  // Önceki hata bildirimleri ekranda kalmış olabilir; TÜM bildirimler aranır.
  await openServer(win, "fake@down");
  const downToast = `[...document.querySelectorAll('[data-testid="toast"] .toast-text')].find((el) => el.textContent.includes("Connection refused"))`;
  await waitFor(win, downToast, "error toast");
  const toast = await evaluate<string>(win, `${downToast}.textContent`);
  expect(toast.startsWith("Bağlanılamadı: could not connect: ssh: connect to host down"), `toast text: ${toast}`);
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
    else await checkWordmark(win);

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

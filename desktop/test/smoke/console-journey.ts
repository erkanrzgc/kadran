/**
 * Kadran Konsolu duman yolculuğu (K-169): sahte kadrand'a (desktop/console_smoke.go,
 * bufconn) karşı konsol panelini kullanıcı gibi sürer. Onay kutusu sahte Host'a
 * gider ve orada kaydedilir; duman bayrağı (Fixture) konsol kutusunu atlatmaz.
 */

import { click, evaluate, expect, fill, noHorizontalOverflow, openServer, q, screenshot, waitFor, type Page } from "./journey.ts";

/** Sahte Host'un kaydettiği onay kutusu (desktop/internal/confirm.Box). */
export interface ConsoleBox {
  Title: string;
  Message: string;
  Detail: string;
  Cancel: string;
  Action: string;
}

/** Sahte kadrand'ın sabit sırrı (console_smoke.go smokeSecret). */
const SECRET = "JBSWY3DPEHPK3PXP";
const RECOVERY = "aaaa-bbbb";
const COMMIT = "0123456789abcdef0123456789abcdef01234567";
/** Panelin arabelleği bu kadar satırı geçmez (lib/consoleBuffer.ts). */
const BUFFER_MAX = 5000;
const INPUT = '[data-testid="console-input"]';
const OUTPUT = '[data-testid="console-output"]';
const CARD = '[data-testid="console-card"]';
/** Gizli kart açıkken istemin yer tutucusu (i18n en.json console.card_first, R17). */
const CARD_FIRST = "Close the secret card first (I saved it).";

interface KeyInit {
  key: string;
  ctrlKey?: boolean;
}

/** Girdi satırına tuş yollar (React'in dinleyicisi kökte: gerçek KeyboardEvent). */
async function press(win: Page, init: KeyInit): Promise<void> {
  const ok = await evaluate<boolean>(win, `(() => { const el = ${q(INPUT)}; if (!el) return false; el.focus(); el.dispatchEvent(new KeyboardEvent("keydown", { bubbles: true, cancelable: true, ...${JSON.stringify(init)} })); return true; })()`);
  expect(ok, "console input is missing");
}

/** Satırı yazar ve Enter'a basar. */
async function typeLine(win: Page, line: string): Promise<void> {
  await fill(win, INPUT, line);
  await press(win, { key: "Enter" });
}

const outputText = (win: Page): Promise<string> => evaluate<string>(win, `${q(OUTPUT)}?.textContent ?? ""`);

/** Son çıktı satırı çıkış kodunu taşıyor ve girdi satırı açık mı. */
const finished = (ok: boolean): string =>
  `(() => { const l = document.querySelectorAll(".console-line"); const last = l[l.length - 1]; const i = ${q(INPUT)}; return Boolean(last && last.classList.contains("${ok ? "is-exit-ok" : "is-exit-bad"}") && i && !i.readOnly); })()`;

/** Komut 0 ile bitti ama gizli kart açık: istem kilitli ve nedenini söylüyor (R17). */
const held = (): string =>
  `(() => { const l = document.querySelectorAll(".console-line"); const last = l[l.length - 1]; const i = ${q(INPUT)}; return Boolean(last && last.classList.contains("is-exit-ok") && i && i.readOnly && i.placeholder === ${JSON.stringify(CARD_FIRST)}); })()`;

/** Komut bir istem bekliyor: girdi açık ve yer tutucu boş (ConsolePrompt). */
const awaiting = () => `(() => { const i = ${q(INPUT)}; return Boolean(i && !i.readOnly && i.placeholder === ""); })()`;

async function clearPanel(win: Page): Promise<void> {
  await press(win, { key: "l", ctrlKey: true });
  await waitFor(win, `(${q(OUTPUT)}?.children.length ?? 1) === 0`, "console output cleared");
}

async function showsText(win: Page, text: string): Promise<boolean> {
  return (await outputText(win)).includes(text);
}

async function waitForText(win: Page, text: string): Promise<void> {
  await waitFor(win, `${q(OUTPUT)}?.textContent.includes(${JSON.stringify(text)})`, `console output "${text}"`);
}

interface CardBox {
  top: number;
  left: number;
  right: number;
  bottom: number;
  width: number;
  height: number;
  vw: number;
  vh: number;
  topHit: boolean;
  bottomHit: boolean;
}

/** Kart görünür alanın içinde ve hiçbir ata onu kırpmıyor (iki köşe noktası kartın kendisi). */
async function expectCardVisible(win: Page, label: string): Promise<void> {
  const b = await evaluate<CardBox>(win, `(() => {
    const card = ${q(CARD)};
    const r = card.getBoundingClientRect();
    const hit = (x, y) => { const e = document.elementFromPoint(x, y); return Boolean(e && card.contains(e)); };
    return { top: r.top, left: r.left, right: r.right, bottom: r.bottom, width: r.width, height: r.height, vw: window.innerWidth, vh: window.innerHeight,
      topHit: hit(r.left + 6, r.top + 6), bottomHit: hit(r.right - 6, r.bottom - 6) };
  })()`);
  const where = `${label}: card ${Math.round(b.left)},${Math.round(b.top)} to ${Math.round(b.right)},${Math.round(b.bottom)} in ${b.vw}x${b.vh}`;
  expect(b.top >= 0 && b.left >= 0 && b.right <= b.vw + 1 && b.bottom <= b.vh + 1, `the secret card is outside the window (${where})`);
  expect(b.topHit && b.bottomHit, `something covers or clips the secret card (${where})`);
}

/** Sır kart dışında hiçbir yerde yok: gövdenin görünen metnindeki sayı kartınkine eşit. */
async function expectSecretOnlyInCard(win: Page): Promise<void> {
  const n = await evaluate<{ body: number; card: number }>(win, `(() => {
    const count = (t) => t.split(${JSON.stringify(SECRET)}).length - 1;
    return { body: count(document.body.innerText), card: count(${q(CARD)}.innerText) };
  })()`);
  expect(n.card >= 1, "the secret is not in the card");
  expect(n.body === n.card, `the secret shows outside the card (${n.body} in the page, ${n.card} in the card)`);
  expect(!(await showsText(win, SECRET)), "the secret reached the console output");
}

/**
 * totp enable (tehlikeli: sunucu adı yazılır): kart açılır, sır yalnız kartta,
 * komut 0 ile bitince kart açık kalır ve kurtarma kodlarını taşır (R14),
 * kart açıkken istem yeni satır almaz (R17), "Kaydettim"den sonra sır DOM'da
 * hiç yok ve istem açılır.
 */
async function totpScenario(win: Page, label: string): Promise<void> {
  await clearPanel(win);
  const before = (await win.boxes()).length;
  await typeLine(win, "totp enable");
  await waitForText(win, "type fake to confirm");
  await waitFor(win, awaiting(), "the typed-name prompt");
  await typeLine(win, "fake");
  await waitFor(win, q(CARD), "the secret card");
  await expectSecretOnlyInCard(win);
  await waitFor(win, awaiting(), "the 6-digit code prompt");
  await typeLine(win, "123456");
  await waitFor(win, held(), "totp enable finished; the prompt waits for the card (R17)");

  const boxes = await win.boxes();
  expect(boxes.length === before + 1, `totp enable asked ${boxes.length - before} confirmation boxes, expected 1`);
  expect((boxes.at(-1)?.Detail ?? "").includes("totp enable"), "the totp box does not show the command");
  const card = await evaluate<string>(win, `${q(CARD)}?.innerText ?? ""`);
  expect(card.includes(RECOVERY), "the card closed before the recovery codes could be read (exit 0 keeps it open)");
  await expectSecretOnlyInCard(win);
  await expectCardVisible(win, label);
  await screenshot(win, `console-card-${label}`);

  await click(win, '[data-testid="console-card-saved"]');
  await waitFor(win, `!${q(CARD)}`, "the card closes after I saved it");
  await waitFor(win, finished(true), "the prompt opens after I saved it");
  const leaked = await evaluate<string[]>(win, `(() => { const html = document.documentElement.outerHTML; return [${JSON.stringify(SECRET)}, ${JSON.stringify(RECOVERY)}].filter((s) => html.includes(s)); })()`);
  expect(leaked.length === 0, `secret text is still in the page after I saved it: ${leaked.join(", ")}`);
}

/** Okuma, değiştirme, akış ve reddedilen komutlar (1280x820). */
async function commandScenarios(win: Page, label: string): Promise<void> {
  // app list: okuma, kutu yok.
  await clearPanel(win);
  let before = (await win.boxes()).length;
  await typeLine(win, "app list");
  await waitFor(win, finished(true), "app list finished");
  expect(await showsText(win, "github.com/demo/web"), "app list output is not in the panel");
  expect((await win.boxes()).length === before, "a read command opened a confirmation box");

  // deploy: kutu SAHTE Host'ta istendi (Fixture atlatmadı), 3 satır aktı, son satır yeşil.
  await clearPanel(win);
  before = (await win.boxes()).length;
  await typeLine(win, `deploy -commit ${COMMIT} web`);
  await waitFor(win, finished(true), "deploy finished");
  const boxes = await win.boxes();
  expect(boxes.length === before + 1, `deploy asked ${boxes.length - before} confirmation boxes on the fake host, expected 1 (the fixture skip must not reach the console)`);
  const box = boxes.at(-1) ?? { Title: "", Message: "", Detail: "", Cancel: "", Action: "" };
  expect(box.Detail.includes(COMMIT) && box.Detail.includes("web"), "the deploy box does not show the whole line");
  expect(`${box.Message}\n${box.Detail}`.includes("fake"), "the deploy box does not name the server");
  for (const step of ["build step 1/3", "build step 2/3", "build step 3/3"]) expect(await showsText(win, step), `deploy output lacks "${step}"`);
  await screenshot(win, `console-${label}`);

  // logs -f + Ctrl+C: akış sürer, istem kilitli, Ctrl+C done getirir ve istem açılır.
  await clearPanel(win);
  await typeLine(win, "logs -f web");
  await waitForText(win, "live line 2");
  expect(await evaluate<boolean>(win, `${q(INPUT)}.readOnly`), "the prompt is not locked while a command runs");
  await press(win, { key: "c", ctrlKey: true });
  await waitFor(win, `(() => { const l = document.querySelectorAll(".console-line"); const last = l[l.length - 1]; const i = ${q(INPUT)}; return Boolean(last && /is-exit-/.test(last.className) && i && !i.readOnly); })()`, "logs -f stopped by Ctrl+C");

  // key list: yönetici komutları ayar kapalıyken reddedilir, kutu da açılmaz.
  await clearPanel(win);
  before = (await win.boxes()).length;
  await typeLine(win, "key list");
  await waitFor(win, finished(false), "key list refused");
  expect(await showsText(win, "admin commands are off"), "key list was not refused with the admin-off reason");
  expect((await win.boxes()).length === before, "a refused command opened a confirmation box");
}

/** 5.000 satırı aşan çıktı: arabellek sınırlı, panel yanıt veriyor. */
async function floodScenario(win: Page): Promise<void> {
  await clearPanel(win);
  await typeLine(win, "logs -tail 6000 web");
  await waitForText(win, "history line 6000");
  await waitFor(win, finished(true), "the flood finished");
  const lines = await evaluate<number>(win, `${q(OUTPUT)}.children.length`);
  expect(lines <= BUFFER_MAX, `the panel holds ${lines} lines, over the ${BUFFER_MAX} line cap`);
}

async function setTheme(win: Page, name: "light" | "dark"): Promise<void> {
  await click(win, '[data-testid="nav-settings"]');
  await click(win, `[data-testid="theme-${name}"]`);
  await waitFor(win, `document.documentElement.dataset.theme === "${name}"`, `${name} theme`);
  await openServer(win, "fake@test");
  await waitFor(win, q('[data-testid="console"]'), "the console panel is open after changing the theme");
}

/** Konsol yolculuğu; bitince açık tema, 1280x820 ve kapalı panel (sonraki adımlar böyle bekler). */
export async function consoleJourney(win: Page): Promise<void> {
  try {
    await consoleSteps(win);
  } catch (err) {
    // Takılan adımın nedenini panelin son çıktısı söyler.
    const tail = (await outputText(win).catch(() => "")).split(String.fromCharCode(10)).slice(-12).join(" | ");
    throw new Error(`${(err as Error).message} (console output: ${tail})`);
  }
}

async function consoleSteps(win: Page): Promise<void> {
  await win.resize(1280, 820);
  await openServer(win, "fake@test");
  if (!(await evaluate<boolean>(win, `Boolean(${q('[data-testid="console"]')})`))) await click(win, '[data-testid="console-open"]');
  await waitFor(win, q('[data-testid="console"]'), "the console panel");

  await commandScenarios(win, "light-1280x820");
  await totpScenario(win, "light-1280x820");
  await floodScenario(win);

  // 760x520: panel ve kart taşmaz, kart pencerenin içinde.
  await win.resize(760, 520);
  await waitFor(win, "Math.abs(window.innerWidth - 760) <= 1", "minimum window size");
  await totpScenario(win, "light-760x520");
  await noHorizontalOverflow(win, "console at 760x520");
  const over = await evaluate<number>(win, `${q(".console")}.scrollWidth - ${q(".console")}.clientWidth`);
  expect(over <= 1, `the console overflows horizontally at 760x520 (${over}px)`);
  await screenshot(win, "console-light-760x520");
  await win.resize(1280, 820);

  // Koyu tema: aynı kart, aynı denetimler.
  await setTheme(win, "dark");
  await totpScenario(win, "dark-1280x820");
  await screenshot(win, "console-dark-1280x820");

  await click(win, '[data-testid="console-close"]');
  await waitFor(win, `!${q('[data-testid="console"]')}`, "the console panel closes");
  await setThemeLight(win);
}

/** Temayı açığa döndürür (themeJourney açık varsayılanı bekler); panel kapalı kalır. */
async function setThemeLight(win: Page): Promise<void> {
  await click(win, '[data-testid="nav-settings"]');
  await click(win, '[data-testid="theme-light"]');
  await waitFor(win, `document.documentElement.dataset.theme === "light"`, "light theme restored");
}

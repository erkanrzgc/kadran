/**
 * Konsol oturumu: çalışma kimliği, istem kilidi, sunucu kilidi ve gizli çıktı
 * kartı. Saf işlevler; arayüz yalnız bunları çağırır.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import type { ConsoleEvent } from "../src/renderer/bridge.ts";
import {
  answer,
  applyEvent,
  begin,
  canRun,
  clearOutput,
  closeCard,
  closePanel,
  initialSession,
  isBusy,
  isRootLine,
  promptServer,
  setPanelOpen,
  started,
  startFailed,
  type ConsoleSession,
} from "../src/renderer/lib/consoleSession.ts";

const NOTE = "secret output shown in a separate card";
const RUN = "0123456789abcdef";
const OTHER = "fedcba9876543210";

const out = (run: string, data: string, stream: "out" | "err" = "out"): ConsoleEvent => ({ name: "console:output", data: { run, stream, data } });
const secret = (run: string, data: string): ConsoleEvent => ({ name: "console:secret", data: { run, data } });
const done = (run: string, exit: number): ConsoleEvent => ({ name: "console:done", data: { run, exit } });
const input = (run: string, prompt: string): ConsoleEvent => ({ name: "console:input", data: { run, prompt } });
const apply = (s: ConsoleSession, e: ConsoleEvent): ConsoleSession => applyEvent(s, e, NOTE);
const allText = (s: ConsoleSession): string => s.buffer.lines.map((l) => l.text).join("\n");

const running = (server = "hetzner", line = "status"): ConsoleSession => started(begin(setPanelOpen(initialSession(), true), server, line), RUN);

test("panel close keeps run, closes card", () => {
  let s = apply(running(), secret(RUN, "KOD-1234"));
  assert.equal(s.card, "KOD-1234");
  s = closePanel(s);
  assert.equal(s.card, null);
  assert.equal(s.run, RUN);
  assert.equal(isBusy(s), true);
  // Panel yeniden açılınca sonraki parça kart açar; kapatma komutu durdurmadı.
  assert.equal(apply(setPanelOpen(s, true), secret(RUN, "kurtarma")).card, "kurtarma");
});

test("secret arriving while the panel is closed never enters state", () => {
  let s = setPanelOpen(running("hetzner", "totp enable"), false);
  s = apply(s, secret(RUN, "KURTARMA-KODU-1\n"));
  s = apply(s, secret(RUN, "KURTARMA-KODU-2\n"));
  assert.equal(s.card, null);
  assert.equal(s.buffer.lines.filter((l) => l.text === NOTE).length, 1);
  assert.equal(JSON.stringify(s).includes("KURTARMA"), false);
  assert.equal(s.run, RUN);
  // Yeniden açınca kısmi kart çıkmaz.
  assert.equal(setPanelOpen(s, true).card, null);
  // Oturum kapalı başlar: açılış senkronundan önce gelen sır da düşer.
  assert.equal(apply(started(begin(initialSession(), "h", "totp enable"), RUN), secret(RUN, "X1")).card, null);
});

test("server change keeps running prompt", () => {
  let s = begin(initialSession(), "hetzner", "deploy web");
  // Başlarken (consoleRun daha dönmedi) de kilitli.
  assert.equal(promptServer(s, "gcp"), "hetzner");
  s = started(s, RUN);
  assert.equal(promptServer(s, "gcp"), "hetzner");
  assert.equal(promptServer(s, "hetzner"), "hetzner");
  s = apply(s, done(RUN, 0));
  assert.equal(promptServer(s, "gcp"), "gcp");
  assert.equal(promptServer(initialSession(), "gcp"), "gcp");
});

test("secret never enters buffer", () => {
  let s = apply(running("hetzner", "totp enable"), out(RUN, "once\n"));
  s = apply(s, secret(RUN, "otpauth://totp/x?secret=ABCDEF\n"));
  s = apply(s, secret(RUN, "QR-SATIRI\n"));
  s = apply(s, secret(RUN, "recovery-1 recovery-2\n"));
  assert.equal(s.card, "otpauth://totp/x?secret=ABCDEF\nQR-SATIRI\nrecovery-1 recovery-2\n");
  const text = allText(s);
  for (const leak of ["otpauth", "ABCDEF", "QR-SATIRI", "recovery"]) assert.equal(text.includes(leak), false, leak);
  // Tamponda yalnız tek bir not satırı var, kart parçası sayısı kadar değil.
  assert.equal(s.buffer.lines.filter((l) => l.text === NOTE).length, 1);
  assert.equal(s.buffer.lines.filter((l) => l.stream === "note").length, 1);
  // Boş parça kart açmaz.
  assert.equal(apply(running(), secret(RUN, "")).card, null);
  // Kapanınca durum null: boş dize değil.
  const closed = closeCard(s);
  assert.equal(closed.card, null);
  assert.notEqual(closed.card, "");
  assert.equal(allText(closed).includes("ABCDEF"), false);
});

test("card stays after exit 0 and closes after a non-zero exit (R14)", () => {
  const withCard = apply(running("hetzner", "totp enable"), secret(RUN, "recovery-1\n"));
  const ok = apply(withCard, done(RUN, 0));
  assert.equal(ok.card, "recovery-1\n");
  assert.equal(isBusy(ok), false);
  // "Kaydettim" ya da panelin kapanması kartı kapatır; içerik durumdan gider.
  assert.equal(closeCard(ok).card, null);
  assert.equal(closePanel(ok).card, null);

  const failed = apply(withCard, done(RUN, 1));
  assert.equal(failed.card, null);
  assert.equal(isBusy(failed), false);
  assert.equal(JSON.stringify(failed).includes("recovery-1"), false);
});

// R17: kart onu üreten çalışmaya aittir. Kart açıkken yeni satır çalışmaz;
// başka bir çalışmanın sıfır dışı bitişi kartı (tek gösterilen kurtarma
// kodlarını) silmez.
const cardOpenAfterExit0 = (): ConsoleSession =>
  apply(apply(running("hetzner", "totp enable"), secret(RUN, "recovery-1\n")), done(RUN, 0));

test("a card from run A survives run B ending non-zero (R17)", () => {
  const a = cardOpenAfterExit0();
  assert.equal(a.card, "recovery-1\n");
  // B, begin'den geçemez (kart açık); B'nin çalıştığı durum elle kurulur
  // (geç dönen consoleRun ya da eski bir çalışmanın olayları).
  for (const exit of [1, 2]) {
    let s: ConsoleSession = { ...a, starting: false, run: OTHER };
    s = apply(s, out(OTHER, "typo\n", "err"));
    assert.equal(s.run, OTHER);
    s = apply(s, done(OTHER, exit));
    assert.equal(s.card, "recovery-1\n", `exit ${exit}`);
    assert.equal(isBusy(s), false);
    // started() yolu da aynı: kimlik dönünce benimsenen çalışma.
    const viaStarted = apply(started({ ...a, starting: true }, OTHER), done(OTHER, exit));
    assert.equal(viaStarted.card, "recovery-1\n", `started, exit ${exit}`);
  }
});

test("a new line is refused while the card is open (R17)", () => {
  const a = cardOpenAfterExit0();
  assert.equal(canRun(a), false);
  // begin kartı korur: yankı yok, başlamıyor, kart aynı.
  const b = begin(a, "hetzner", "totp enable");
  assert.equal(b, a);
  assert.equal(isBusy(b), false);
  assert.equal(b.card, "recovery-1\n");
  assert.equal(allText(b).includes("totp enable"), allText(a).includes("totp enable"));
  // Çalışırken (kod istemi) kart açık olsa da satır zaten kilitli: canRun yine false.
  const midRun = apply(running("hetzner", "totp enable"), secret(RUN, "SECRET\n"));
  assert.equal(canRun(midRun), false);
  assert.equal(canRun(initialSession()), true);
});

test("after I saved it a new line runs (R17)", () => {
  const saved = closeCard(cardOpenAfterExit0());
  assert.equal(saved.card, null);
  assert.equal(canRun(saved), true);
  const next = started(begin(saved, "hetzner", "status"), OTHER);
  assert.equal(next.run, OTHER);
  assert.ok(allText(next).includes("hetzner › status"));
  // Kapanan kartın kimliği de gitti: yeni çalışmanın sırrı yeni kart açar.
  const fresh = apply(next, secret(OTHER, "new-secret\n"));
  assert.equal(fresh.card, "new-secret\n");
  assert.equal(apply(fresh, done(OTHER, 1)).card, null);
  // Panelin kapanması da kartı kapatır ve satırın önünü açar.
  const closed = closePanel(cardOpenAfterExit0());
  assert.equal(closed.card, null);
  assert.equal(canRun(closed), true);
});

test("the card still closes when its own run ends non-zero (R17)", () => {
  const withCard = apply(running("hetzner", "totp enable"), secret(RUN, "recovery-1\n"));
  for (const exit of [1, 2, 130]) {
    const failed = apply(withCard, done(RUN, exit));
    assert.equal(failed.card, null, `exit ${exit}`);
    assert.equal(canRun(failed), true);
    assert.equal(JSON.stringify(failed).includes("recovery-1"), false);
  }
});

test("events before consoleRun resolves are not dropped", () => {
  let s = begin(initialSession(), "hetzner", "status");
  s = apply(s, out(RUN, "ilk\n"));
  s = apply(s, input(RUN, "kod: "));
  assert.equal(s.run, RUN);
  assert.equal(s.input, true);
  s = started(s, RUN);
  assert.equal(s.run, RUN);
  assert.equal(isBusy(s), true);
  assert.ok(allText(s).includes("ilk"));
});

test("a run that finishes before consoleRun resolves does not relock the prompt", () => {
  let s = begin(initialSession(), "hetzner", "status");
  s = apply(s, out(RUN, "tamam\n"));
  s = apply(s, done(RUN, 0));
  assert.equal(isBusy(s), true, "consoleRun henüz dönmedi");
  s = started(s, RUN);
  assert.equal(isBusy(s), false);
  assert.equal(s.run, null);
  // Done sonrası gelen output eklenir, istemi yeniden kilitlemez.
  s = apply(s, out(RUN, "geç\n"));
  assert.equal(isBusy(s), false);
  assert.ok(allText(s).includes("geç"));
});

test("output after done is appended without reopening or relocking", () => {
  let s = apply(running(), out(RUN, "bir\n"));
  s = apply(s, done(RUN, 0));
  s = apply(s, out(RUN, "sonradan\n"));
  assert.equal(isBusy(s), false);
  assert.equal(s.run, null);
  assert.ok(allText(s).includes("sonradan"));
  // Eski çalışmanın done'u yeni çalışmayı açmaz.
  const next = started(begin(s, "hetzner", "status"), OTHER);
  assert.equal(apply(next, done(RUN, 0)).run, OTHER);
  assert.equal(apply(next, out(RUN, "eski\n")).run, OTHER);
});

test("answering a prompt never puts the typed text anywhere", () => {
  let s = apply(running("hetzner", "login"), out(RUN, "TOTP code: "));
  s = apply(s, input(RUN, "TOTP code: "));
  assert.equal(s.input, true);
  assert.equal(s.buffer.lines.at(-1)!.open, true);
  // answer() metin almaz; istem satırı yerinde bitirilir.
  assert.equal(answer.length, 1);
  s = answer(s);
  assert.equal(s.input, false);
  assert.equal(s.buffer.lines.at(-1)!.open, false);
  assert.equal(s.buffer.lines.at(-1)!.text, "TOTP code: ");
  assert.equal(s.buffer.lines.length, 2);
});

test("a stray input event for another run is ignored", () => {
  const s = apply(running(), input(OTHER, "x: "));
  assert.equal(s.input, false);
});

test("begin echoes the line; done marks the echo line, not the output", () => {
  let s = begin(initialSession(), "hetzner", "status");
  assert.equal(s.buffer.lines[0]!.text, "hetzner › status");
  assert.equal(s.buffer.lines[0]!.stream, "echo");
  s = apply(started(s, RUN), out(RUN, "ok\n"));
  s = apply(s, done(RUN, 1));
  assert.equal(s.buffer.lines[0]!.exit, 1);
  assert.equal(s.buffer.lines.at(-1)!.exit, null);
});

test("startFailed unlocks and shows the error", () => {
  const s = startFailed(begin(initialSession(), "hetzner", "status"), "busy");
  assert.equal(isBusy(s), false);
  assert.equal(s.buffer.lines.at(-1)!.text, "busy");
  assert.equal(s.buffer.lines.at(-1)!.stream, "err");
});

test("clear event and Ctrl+L empty the buffer; the run state stays", () => {
  const s = apply(running(), out(RUN, "x\n"));
  assert.deepEqual(clearOutput(s).buffer.lines, []);
  const cleared = apply(s, { name: "console:clear", data: {} });
  assert.deepEqual(cleared.buffer.lines, []);
  assert.equal(cleared.run, RUN);
});

test("the prompt shows # only while a root-class line runs", () => {
  assert.equal(isRootLine("key list"), true);
  assert.equal(isRootLine("kadran key add ./k.pub"), true);
  assert.equal(isRootLine("totp reset"), true);
  assert.equal(isRootLine("totp status"), false);
  assert.equal(isRootLine("status"), false);
  assert.equal(begin(initialSession(), "hetzner", "key list").root, true);
  assert.equal(begin(initialSession(), "hetzner", "status").root, false);
});

test("clear during run never marks output", () => {
  let s = begin(initialSession(), "hetzner", "logs -f web");
  s = apply(started(s, RUN), out(RUN, "a\nb\n"));
  s = clearOutput(s);
  s = apply(s, out(RUN, "c\nd\n"));
  s = apply(s, done(RUN, 130));
  assert.deepEqual(s.buffer.lines.map((l) => l.exit), [null, null]);
});

test("line ids stay unique across a clear", () => {
  let s = begin(initialSession(), "hetzner", "status");
  s = apply(started(s, RUN), out(RUN, "a\nb\n"));
  const before = s.buffer.lines.map((l) => l.id);
  s = apply(clearOutput(s), out(RUN, "c\nd\n"));
  const all = [...before, ...s.buffer.lines.map((l) => l.id)];
  assert.equal(new Set(all).size, all.length);
});

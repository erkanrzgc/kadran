/**
 * Konsol çıktı tamponu: satır ve satır uzunluğu sınırı, akışın parça parça
 * gelmesi, terminal denetim karakterlerinin ayıklanması.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { append, emptyBuffer, endLine, finish, MAX_LINE_CHARS, MAX_LINES, TRUNCATION_MARK } from "../src/renderer/lib/consoleBuffer.ts";

const texts = (state: ReturnType<typeof emptyBuffer>): string[] => state.lines.map((l) => l.text);

test("consoleBuffer caps lines", () => {
  let state = emptyBuffer();
  for (let i = 0; i <= MAX_LINES; i++) state = append(state, "out", `satir ${i}\n`);
  assert.equal(MAX_LINES, 5000);
  assert.equal(state.lines.length, 5000);
  assert.equal(state.lines[0]!.text, "satir 1");
  assert.equal(state.lines.at(-1)!.text, "satir 5000");

  // Tek olayda gelen çok satır da aynı sınıra takılır.
  const bulk = append(emptyBuffer(), "out", Array.from({ length: 5001 }, (_, i) => `x${i}`).join("\n") + "\n");
  assert.equal(bulk.lines.length, 5000);
  assert.equal(bulk.lines[0]!.text, "x1");
});

test("consoleBuffer caps line length", () => {
  const huge = "a".repeat(10 * 1024 * 1024);
  const state = append(emptyBuffer(), "out", huge);
  assert.equal(state.lines.length, 1);
  assert.equal(state.lines[0]!.text.length, 8 * 1024 + TRUNCATION_MARK.length);
  assert.equal(MAX_LINE_CHARS, 8192);
  assert.ok(state.lines[0]!.text.endsWith(TRUNCATION_MARK));
  assert.equal(TRUNCATION_MARK, "…");
  // Kesilen satırın devamı da düşer; sonraki satır etkilenmez.
  const next = append(state, "out", "daha\nson\n");
  assert.equal(next.lines[0]!.text.length, 8 * 1024 + 1);
  assert.deepEqual(texts(next).slice(1), ["son"]);
});

test("consoleBuffer joins chunks of the same stream into one line", () => {
  let state = append(emptyBuffer(), "out", "par");
  state = append(state, "out", "ca\nikinci");
  state = append(state, "out", " satir\n");
  assert.deepEqual(texts(state), ["parca", "ikinci satir"]);
  assert.equal(
    state.lines.every((l) => !l.open),
    true,
  );
});

test("consoleBuffer starts a new line when the stream changes", () => {
  let state = append(emptyBuffer(), "out", "yarim");
  state = append(state, "err", "hata\n");
  assert.deepEqual(texts(state), ["yarim", "hata"]);
  assert.deepEqual(
    state.lines.map((l) => l.stream),
    ["out", "err"],
  );
  assert.equal(state.lines[0]!.open, false);
});

test("consoleBuffer keeps blank lines and drops CR, ANSI and control characters", () => {
  const esc = String.fromCharCode(27);
  const bell = String.fromCharCode(7);
  const rlo = String.fromCharCode(0x202e);
  const state = append(emptyBuffer(), "out", `a\r\n\nb${esc}[31mred${esc}[0m${bell}${rlo}!\n`);
  assert.deepEqual(texts(state), ["a", "", "bred!"]);
  assert.equal(append(state, "out", "").lines.length, 3);
});

function echoRun() {
  let b = append(emptyBuffer(), "echo", "hetzner › app list\n");
  b = append(b, "out", "a\nb\nc\n");
  return { b, echoId: b.lines[0]!.id };
}

test("finish marks the command line, not output", () => {
  const { b, echoId } = echoRun();
  const done = finish(b, echoId, 0);
  assert.equal(done.lines[0]!.exit, 0);
  assert.deepEqual(done.lines.slice(1).map((l) => l.exit), [null, null, null]);
});

test("failed run adds exit suffix", () => {
  const { b, echoId } = echoRun();
  const done = finish(b, echoId, 2);
  assert.equal(done.lines[0]!.exit, 2);
  assert.equal(done.lines[0]!.text, "hetzner › app list");
  assert.deepEqual(done.lines.slice(1).map((l) => l.exit), [null, null, null]);
});

test("finish closes the open last line without colouring it", () => {
  const { b, echoId } = echoRun();
  const done = finish(append(b, "out", "istem: "), echoId, 0);
  assert.equal(done.lines.at(-1)!.open, false);
  assert.equal(done.lines.at(-1)!.exit, null);
});

test("late output after done stays plain", () => {
  const { b, echoId } = echoRun();
  const late = append(finish(b, echoId, 0), "out", "late\n");
  assert.equal(late.lines.at(-1)!.text, "late");
  assert.equal(late.lines.at(-1)!.exit, null);
});

test("echo line dropped by the 5000 cap", () => {
  const { b, echoId } = echoRun();
  let big = b;
  for (let i = 0; i < MAX_LINES; i++) big = append(big, "out", `x${i}\n`);
  assert.equal(big.lines.some((l) => l.id === echoId), false);
  const done = finish(big, echoId, 1);
  assert.deepEqual(done.lines.map((l) => l.exit), big.lines.map((l) => l.exit));
  assert.deepEqual(finish(emptyBuffer(), 0, 0).lines, []);
});

test("consoleBuffer endLine closes the open prompt line without adding text", () => {
  const open = append(emptyBuffer(), "out", "type the name: ");
  const closed = endLine(open);
  assert.equal(closed.lines.length, 1);
  assert.equal(closed.lines[0]!.text, "type the name: ");
  assert.equal(closed.lines[0]!.open, false);
  assert.equal(endLine(closed), closed);
});

test("consoleBuffer never mutates the previous state", () => {
  const before = append(emptyBuffer(), "out", "bir\nyari");
  const snapshot = JSON.stringify(before);
  append(before, "out", "m\nuc\n");
  finish(before, 1);
  endLine(before);
  assert.equal(JSON.stringify(before), snapshot);
});

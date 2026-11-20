/**
 * Konsol kısayolları, Tab tamamlama ve panel yüksekliği. Ctrl+C çalışma
 * varken durdurur, yokken seçili metnin kopyalanmasına bırakılır (null).
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { action, applyCompletion, shouldFocusPrompt, type KeyLike } from "../src/renderer/lib/consoleKeys.ts";
import { clampHeight, DEFAULT_HEIGHT, MAX_RATIO, MIN_HEIGHT } from "../src/renderer/lib/consoleLayout.ts";

const key = (init: Partial<KeyLike> & { key: string }): KeyLike => ({ ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, running: false, ...init });

test("consoleKeys", () => {
  assert.equal(action(key({ key: "`", ctrlKey: true })), "toggle");
  assert.equal(action(key({ key: "Dead", code: "Backquote", ctrlKey: true })), "toggle");
  assert.equal(action(key({ key: "`" })), null);
  assert.equal(action(key({ key: "Escape" })), null);
  assert.equal(action(key({ key: "c", ctrlKey: true, running: true })), "cancel");
  assert.equal(action(key({ key: "C", ctrlKey: true, running: true })), "cancel");
  assert.equal(action(key({ key: "c", ctrlKey: true, running: false })), null);
  assert.equal(action(key({ key: "c", ctrlKey: true, shiftKey: true, running: true })), null);
  assert.equal(action(key({ key: "l", ctrlKey: true })), "clear");
  assert.equal(action(key({ key: "Tab" })), "complete");
  assert.equal(action(key({ key: "Tab", shiftKey: true })), null);
  assert.equal(action(key({ key: "Enter" })), "run");
  assert.equal(action(key({ key: "ArrowUp" })), "history-up");
  assert.equal(action(key({ key: "ArrowDown" })), "history-down");
  assert.equal(action(key({ key: "ArrowUp", altKey: true })), null);
  assert.equal(action(key({ key: "a" })), null);
});

test("consoleKeys completion replaces the word before the cursor", () => {
  assert.deepEqual(applyCompletion("app sh", 6, ["show"]), { line: "app show ", cursor: 9, candidates: [] });
  assert.deepEqual(applyCompletion("app ", 4, ["create", "delete"]), { line: "app ", cursor: 4, candidates: ["create", "delete"] });
  assert.deepEqual(applyCompletion("dep", 3, ["deploy", "deploy-all"]), { line: "deploy", cursor: 6, candidates: ["deploy", "deploy-all"] });
  assert.deepEqual(applyCompletion("lo web", 2, ["logs"]), { line: "logs  web", cursor: 5, candidates: [] });
  assert.deepEqual(applyCompletion("zz", 2, []), { line: "zz", cursor: 2, candidates: [] });
});

test("shouldFocusPrompt keeps the input focused while the panel is open, also while a run continues after an answer", () => {
  assert.equal(shouldFocusPrompt(true), true);
  assert.equal(shouldFocusPrompt(false), false);
});

test("consoleLayout clamps the panel height to 120 px .. 70% of the window", () => {
  assert.equal(MIN_HEIGHT, 120);
  assert.equal(MAX_RATIO, 0.7);
  assert.equal(DEFAULT_HEIGHT, 240);
  assert.equal(clampHeight(50, 1000), 120);
  assert.equal(clampHeight(500, 1000), 500);
  assert.equal(clampHeight(900, 1000), 700);
  // Pencere çok küçükse alt sınır kazanır.
  assert.equal(clampHeight(300, 100), 120);
  assert.equal(clampHeight(Number.NaN, 1000), 240);
});

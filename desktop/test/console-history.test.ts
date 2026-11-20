/**
 * Konsol geçmişi gezintisi: ↑ son satır, ↑↑ bir öncekisi, ↓ yazılmakta olan
 * taslağa döner. Geçmişin kendisi Go'dan gelir (arıtılmış); burada yalnız gezinti.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { createHistory, current, edit, move } from "../src/renderer/lib/consoleHistory.ts";

test("consoleHistory navigates", () => {
  let h = edit(createHistory(["status", "app list", "logs web"]), "dep");
  h = move(h, "up");
  assert.equal(current(h), "logs web");
  h = move(h, "up");
  assert.equal(current(h), "app list");
  h = move(h, "down");
  assert.equal(current(h), "logs web");
  h = move(h, "down");
  assert.equal(current(h), "dep");
  assert.equal(current(move(h, "down")), "dep");
});

test("consoleHistory stops at the oldest line and with an empty history", () => {
  let h = createHistory(["bir", "iki"]);
  for (let i = 0; i < 5; i++) h = move(h, "up");
  assert.equal(current(h), "bir");
  const none = createHistory([]);
  assert.equal(move(none, "up"), none);
  assert.equal(current(none), "");
});

test("consoleHistory typing after a recall makes that text the draft", () => {
  let h = move(createHistory(["status"]), "up");
  h = edit(h, "status -s web");
  assert.equal(current(h), "status -s web");
  h = move(h, "up");
  assert.equal(current(h), "status");
  h = move(h, "down");
  assert.equal(current(h), "status -s web");
});

test("consoleHistory never mutates the previous state", () => {
  const before = createHistory(["a", "b"]);
  const snapshot = JSON.stringify(before);
  move(before, "up");
  edit(before, "x");
  assert.equal(JSON.stringify(before), snapshot);
});

/**
 * R6: çıkıştan sonra okumalar düşerken rozet "Healthy" kalıyordu. Bayatlık
 * yalnız Durum ekranının yenilemesinden geliyordu; filo yoklaması etkin
 * sunucunun düştüğünü görüyor ama rozete iletmiyordu. Etkin sunucunun
 * kartı düşünce (ya da düzelince) durum BİR kez yenilenir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import type { Profile } from "../src/renderer/bridge.ts";
import { cardOf, type CardData } from "../src/renderer/lib/fleet.ts";
import { fleetFailed, shouldRefresh } from "../src/renderer/lib/fleet-follow.ts";
import type { StatusPayload } from "../src/renderer/lib/types.ts";

const status = { executor_reachable: true } as StatusPayload;

function card(target: string, data: CardData) {
  return cardOf({ target } as Profile, data);
}

test("fleetFailed reads only the active server's card", () => {
  const cards = [card("a@x", { failure: 'session expired: sign in again with "kadran login"' }), card("b@x", { status })];
  assert.equal(fleetFailed(cards, "a@x"), true);
  assert.equal(fleetFailed(cards, "b@x"), false);
});

test("fleetFailed has no answer without a target, a card, or a first reading", () => {
  const cards = [card("a@x", {}), card("b@x", { status })];
  assert.equal(fleetFailed(cards, null), null);
  assert.equal(fleetFailed(cards, "missing@x"), null);
  assert.equal(fleetFailed(cards, "a@x"), null);
});

test("a fleet failure on the same server asks for one status refresh", () => {
  assert.equal(shouldRefresh({ target: "a@x", failed: false }, { target: "a@x", failed: true }), true);
});

test("a fleet recovery on the same server asks for one status refresh", () => {
  assert.equal(shouldRefresh({ target: "a@x", failed: true }, { target: "a@x", failed: false }), true);
});

test("an unchanged reading does not refresh again", () => {
  assert.equal(shouldRefresh({ target: "a@x", failed: true }, { target: "a@x", failed: true }), false);
  assert.equal(shouldRefresh({ target: "a@x", failed: false }, { target: "a@x", failed: false }), false);
});

test("switching servers or a first reading does not refresh", () => {
  assert.equal(shouldRefresh({ target: "a@x", failed: false }, { target: "b@x", failed: true }), false);
  assert.equal(shouldRefresh({ target: "a@x", failed: null }, { target: "a@x", failed: true }), false);
  assert.equal(shouldRefresh({ target: "a@x", failed: false }, { target: "a@x", failed: null }), false);
  assert.equal(shouldRefresh({ target: null, failed: null }, { target: null, failed: null }), false);
});

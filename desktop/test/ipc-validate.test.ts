/** Renderer güvenilmez kaynaktır: her IPC girdisi ana süreçte doğrulanır. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { MAX_COPY, requireCopyText, requireTarget, requireWindowAction, toUint } from "../src/main/ipc-validate.ts";

test("hedef: boş, boşluk ve 500 karakterden uzun reddedilir; kırpılır", () => {
  assert.throws(() => requireTarget(""), /target is empty/);
  assert.throws(() => requireTarget("   "), /target is empty/);
  assert.throws(() => requireTarget(42), /target is empty/);
  assert.throws(() => requireTarget("a".repeat(501)), /target is too long/);
  assert.equal(requireTarget("  kadran-client@host  "), "kadran-client@host");
});

test("hedef: '-' ile başlayan reddedilir (kırpmadan sonra), içinde '-' olan geçer", () => {
  assert.throws(() => requireTarget("-oProxyCommand=x"), /must not start with/);
  assert.throws(() => requireTarget("  -x@host"), /must not start with/);
  assert.equal(requireTarget("user@host-1"), "user@host-1");
  assert.equal(requireTarget("a-b@c"), "a-b@c");
});

test("toUint: negatif, sonsuz ve sayı olmayan → varsayılan; kesir aşağı", () => {
  assert.equal(toUint(-1, 7), 7);
  assert.equal(toUint(Number.POSITIVE_INFINITY, 7), 7);
  assert.equal(toUint("x", 7), 7);
  assert.equal(toUint("12", 7), 12);
  assert.equal(toUint(3.9, 7), 3);
});

test("pencere eylemi beyaz listede", () => {
  assert.equal(requireWindowAction("minimize"), "minimize");
  assert.equal(requireWindowAction("maximize"), "maximize");
  assert.equal(requireWindowAction("close"), "close");
  assert.throws(() => requireWindowAction("devtools"), /unknown window action/);
  assert.throws(() => requireWindowAction(null), /unknown window action/);
});

test("kopyalama metni 1…1024 karakter", () => {
  assert.equal(requireCopyText("SHA256:abc"), "SHA256:abc");
  assert.throws(() => requireCopyText(""), /1-1024/);
  assert.throws(() => requireCopyText("x".repeat(MAX_COPY + 1)), /1-1024/);
  assert.throws(() => requireCopyText({}), /1-1024/);
});

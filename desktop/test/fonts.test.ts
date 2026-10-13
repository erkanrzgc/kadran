/**
 * Yazı tipi listesi. Seçilen ailenin her yüzü kendi kalınlığıyla
 * kaydedilir; böylece "Medium" gerçekten Medium çizilir. Liste boşsa
 * (izin yok, API yok) her şey varsayılanla çalışmaya devam eder.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import { cssLocalSource, groupFaces, nearestWeight, parseStyle, strongerWeight, weightsOf } from "../src/renderer/lib/fonts.ts";

test("stil adı → kalınlık ve italik", () => {
  assert.deepEqual(parseStyle("Medium"), { weight: 500, italic: false });
  assert.deepEqual(parseStyle("Thin Italic"), { weight: 100, italic: true });
  assert.deepEqual(parseStyle("SemiBold"), { weight: 600, italic: false });
  assert.deepEqual(parseStyle("Extra-Light"), { weight: 200, italic: false });
  assert.deepEqual(parseStyle("Italic"), { weight: 400, italic: true });
  assert.deepEqual(parseStyle("Regular"), { weight: 400, italic: false });
  assert.deepEqual(parseStyle("Garip Stil"), { weight: 400, italic: false });
});

const LIST = [
  { family: "JetBrainsMono Nerd Font", style: "Medium", postscriptName: "JetBrainsMonoNF-Medium" },
  { family: "Segoe UI", style: "Regular", postscriptName: "SegoeUI" },
  { family: "Segoe UI", style: "Bold", postscriptName: "SegoeUI-Bold" },
  { family: "Segoe UI", style: "Light Italic", postscriptName: "SegoeUI-LightItalic" },
  { family: "Segoe UI", style: "Semibold", postscriptName: "SegoeUI-Semibold" },
];

test("aileye göre gruplar", () => {
  const map = groupFaces(LIST);
  assert.deepEqual([...map.keys()].sort(), ["JetBrainsMono Nerd Font", "Segoe UI"]);
  assert.equal(map.get("Segoe UI")!.length, 4);
  assert.equal(map.get("JetBrainsMono Nerd Font")![0]!.weight, 500);
});

test("kalınlıklar: italik hariç, tekil, artan; boşsa 400", () => {
  const map = groupFaces(LIST);
  assert.deepEqual(weightsOf(map.get("Segoe UI")!), [400, 600, 700]);
  assert.deepEqual(weightsOf(map.get("JetBrainsMono Nerd Font")!), [500]);
  assert.deepEqual(weightsOf([]), [400]);
});

test("en yakın kalınlık ve bir üstü", () => {
  const segoe = groupFaces(LIST).get("Segoe UI")!;
  const jb = groupFaces(LIST).get("JetBrainsMono Nerd Font")!;
  assert.equal(nearestWeight(jb, 400), 500);
  assert.equal(nearestWeight(segoe, 650), 600);
  assert.equal(nearestWeight([], 450), 450);
  assert.equal(strongerWeight(segoe, 400), 600);
  assert.equal(strongerWeight(segoe, 700), 700);
  assert.equal(strongerWeight(jb, 500), 500);
});

test("boş liste güvenli", () => {
  assert.equal(groupFaces([]).size, 0);
});

test("local() kaynağı tırnak ve ters bölüyü kaçırır", () => {
  assert.equal(cssLocalSource("JetBrainsMonoNF-Medium"), 'local("JetBrainsMonoNF-Medium")');
  assert.equal(cssLocalSource('Kötü"Ad\\'), 'local("Kötü\\"Ad\\\\")');
});

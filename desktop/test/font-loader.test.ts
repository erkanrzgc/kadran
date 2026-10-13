/**
 * Yazı tipi yükleyici. Eşzamanlı kayıtlar aynı takma ad için yarışabilir;
 * son çağrı kazanır ve eski yüzler kalmaz.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

// Mock FontFace with controllable delay
const fontFacesByCall: FontFace[][] = [];

class MockFontFace implements FontFace {
  family: string;
  style!: string;
  weight!: string;
  stretch!: string;
  unicodeRange!: string;
  variant!: string;
  featureSettings!: string;
  variationSettings!: string;
  display!: string;
  ascentOverride!: string;
  descentOverride!: string;
  lineGapOverride!: string;
  featurePalette!: string;
  loaded!: Promise<FontFace[]>;

  constructor(family: string, _source: string, _desc?: FontFaceDescriptors) {
    this.family = family;
    fontFacesByCall[fontFacesByCall.length - 1]?.push(this);
  }

  async load(): Promise<FontFace> {
    return this;
  }
}

// Mock document.fonts
const addedFaces = new Set<FontFace>();
const mockDocument = {
  fonts: {
    add(f: FontFace) {
      addedFaces.add(f);
    },
    delete(f: FontFace) {
      addedFaces.delete(f);
    },
  },
};

// Mock window
const mockWindow = {
  queryLocalFonts: async () => [
    { family: "Test", style: "Regular", postscriptName: "Test-Regular" },
  ],
};

// Helper to set up and tear down stubs
async function withStubs<T>(fn: () => Promise<T>): Promise<T> {
  const oldWindow = (globalThis as unknown as Record<string, unknown>).window;
  const oldDocument = (globalThis as unknown as Record<string, unknown>).document;
  const oldFontFace = (globalThis as unknown as Record<string, unknown>).FontFace;

  try {
    (globalThis as unknown as Record<string, unknown>).window = mockWindow;
    (globalThis as unknown as Record<string, unknown>).document = mockDocument;
    (globalThis as unknown as Record<string, unknown>).FontFace = MockFontFace;

    // Clear state
    addedFaces.clear();
    fontFacesByCall.length = 0;

    return await fn();
  } finally {
    (globalThis as unknown as Record<string, unknown>).window = oldWindow;
    (globalThis as unknown as Record<string, unknown>).document = oldDocument;
    (globalThis as unknown as Record<string, unknown>).FontFace = oldFontFace;
  }
}

test("eşzamanlı iki kayıtta son çağrı kazanır, eski yüzler kalmaz", async () => {
  await withStubs(async () => {
    const { registerFamily } = await import("../src/renderer/lib/font-loader.ts");

    const facesA = [
      { family: "A", style: "Regular", postscriptName: "A-Regular", weight: 400, italic: false },
      { family: "A", style: "Bold", postscriptName: "A-Bold", weight: 700, italic: false },
    ];
    const facesB = [
      { family: "B", style: "Regular", postscriptName: "B-Regular", weight: 400, italic: false },
    ];

    const alias = `concurrent-${Math.random()}`;

    fontFacesByCall.push([]); // Call 1
    const promise1 = registerFamily(alias, facesA);

    // Simulate call 1 is awaiting load()
    fontFacesByCall.push([]); // Call 2
    const promise2 = registerFamily(alias, facesB);

    const count1 = await promise1;
    const count2 = await promise2;

    // Second call should return its loaded count
    assert.equal(count2, 1, "second call should report 1 face loaded");
    // First call should return 0 (superseded)
    assert.equal(count1, 0, "first call should return 0 (superseded by newer call)");
    // Only facesB should remain in document.fonts
    assert.equal(addedFaces.size, 1, "only second call's faces should be in document.fonts");
    const remainingFace = [...addedFaces][0];
    assert.equal(remainingFace?.family, alias, "remaining face should have alias family");
  });
});

test("yeniden kayıt eskisini siler", async () => {
  await withStubs(async () => {
    const { registerFamily } = await import("../src/renderer/lib/font-loader.ts");

    const facesA = [
      { family: "A", style: "Regular", postscriptName: "A-Regular", weight: 400, italic: false },
    ];
    const facesB = [
      { family: "B", style: "Regular", postscriptName: "B-Regular", weight: 400, italic: false },
    ];

    const alias = `sequential-${Math.random()}`;

    fontFacesByCall.push([]); // Call 1
    const count1 = await registerFamily(alias, facesA);
    assert.equal(count1, 1, "first registration should load 1 face");
    assert.equal(addedFaces.size, 1, "1 face should be in document.fonts after first call");

    fontFacesByCall.push([]); // Call 2
    const count2 = await registerFamily(alias, facesB);
    assert.equal(count2, 1, "second registration should load 1 face");
    assert.equal(addedFaces.size, 1, "still only 1 face in document.fonts");

    // Verify it's facesB's face
    const remainingFace = [...addedFaces][0];
    assert.equal(remainingFace?.family, alias, "remaining face should be from second registration");
  });
});

test("listLocalFonts: api yok, hata, başarılı", async () => {
  await withStubs(async () => {
    const { listLocalFonts } = await import("../src/renderer/lib/font-loader.ts");

    // Case 1: queryLocalFonts missing
    (globalThis as unknown as Record<string, unknown>).window = {};
    let result = await listLocalFonts();
    assert.deepEqual(result, [], "missing queryLocalFonts returns empty array");

    // Case 2: queryLocalFonts throws
    (globalThis as unknown as Record<string, unknown>).window = {
      queryLocalFonts: async () => {
        throw new Error("permission denied");
      },
    };
    result = await listLocalFonts();
    assert.deepEqual(result, [], "throwing queryLocalFonts returns empty array");

    // Case 3: queryLocalFonts succeeds
    (globalThis as unknown as Record<string, unknown>).window = mockWindow;
    result = await listLocalFonts();
    assert.equal(result.length, 1, "successful queryLocalFonts returns list");
    assert.equal(result[0]?.family, "Test", "returned face has correct family");
  });
});

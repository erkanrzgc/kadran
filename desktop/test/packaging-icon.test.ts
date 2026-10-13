import assert from "node:assert/strict";
import { test } from "node:test";
import { createHash } from "node:crypto";
import { existsSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { inflateSync } from "node:zlib";

const asset = (path: string) => fileURLToPath(new URL(path, import.meta.url));
const ico = asset("../assets/kadran.ico");
const hash = (bytes: Buffer) => createHash("sha256").update(bytes).digest("hex");

test("Windows icon contains all standard sizes as valid alpha PNG frames", () => {
  assert.ok(existsSync(ico), "Windows package icon is missing");
  const bytes = readFileSync(ico);
  assert.equal(bytes.readUInt16LE(0), 0);
  assert.equal(bytes.readUInt16LE(2), 1);
  const count = bytes.readUInt16LE(4);
  assert.equal(count, 7);
  const sizes: number[] = [];
  for (let index = 0; index < count; index++) {
    const entry = 6 + index * 16;
    const size = bytes[entry] || 256;
    sizes.push(size);
    assert.equal(bytes[entry + 1] || 256, size);
    const length = bytes.readUInt32LE(entry + 8);
    const offset = bytes.readUInt32LE(entry + 12);
    assert.ok(offset >= 6 + count * 16 && offset + length <= bytes.length);
    const frame = bytes.subarray(offset, offset + length);
    assert.equal(frame.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
    assert.equal(frame.readUInt32BE(16), size);
    assert.equal(frame.readUInt32BE(20), size);
    assert.equal(frame[25], 6, "Icon must retain RGBA transparency");
    assert.equal(frame[24], 8);
    assert.equal(frame[28], 0, "Expected noninterlaced PNG");
    const chunks: Buffer[] = [];
    for (let offset = 8; offset < frame.length;) {
      const length = frame.readUInt32BE(offset);
      if (frame.subarray(offset + 4, offset + 8).toString() === "IDAT") chunks.push(frame.subarray(offset + 8, offset + 8 + length));
      offset += length + 12;
    }
    // For the first pixel of the first RGBA row, every PNG predictor is zero.
    const decoded = inflateSync(Buffer.concat(chunks));
    assert.equal(decoded[4], 0, "Transparent icon corner must remain transparent");
  }
  assert.deepEqual(sizes, [16, 24, 32, 48, 64, 128, 256]);
});

test("icon provenance binds the outlined K master, native PNG, and generated ICO bytes", () => {
  const path = asset("../assets/icon-provenance.json");
  assert.ok(existsSync(path), "Native icon provenance is missing");
  const manifest = JSON.parse(readFileSync(path, "utf8"));
  assert.equal(manifest.source, "../public/kadran-icon.png");
  assert.equal(manifest.master, "kadran-icon.svg");
  const master = readFileSync(asset("../assets/kadran-icon.svg"));
  assert.equal(hash(master), manifest.master_sha256);
  const source = readFileSync(asset("../public/kadran-icon.png"));
  assert.equal(hash(source), manifest.source_sha256);
  assert.equal(hash(readFileSync(ico)), manifest.ico_sha256);
  assert.equal(source.readUInt32BE(16), 256);
  assert.equal(source.readUInt32BE(20), 256);
  assert.equal(source[25], 6, "Native source must retain RGBA transparency");
  assert.equal(manifest.font, "Bahnschrift Light SemiCondensed");
  assert.deepEqual(manifest.font_axes, { wght: 300, wdth: 87.5 });
  assert.equal(manifest.glyph, "K");
  assert.equal(manifest.fill, "#5B96BA");
  assert.equal(manifest.transform, "RGBA format conversion and LANCZOS resizing only; native source PNG unchanged");
  assert.equal((master.toString("utf8").match(/<path\b/g) ?? []).length, 1, "Native icon must contain one outlined glyph");
  assert.ok(!/<(?:text|image)\b/.test(master.toString("utf8")), "Icon must not require a runtime font or image");
});

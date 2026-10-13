/**
 * Arayüz kodunda çeviri dışı görünür metin yok: JSX metni ve görünür
 * nitelikler (title, aria-label, placeholder, alt) t() ile gelmeli.
 * TypeScript AST'si üstünde çalışır; yorumlar ve kod içi dizeler sayılmaz.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

const here = dirname(fileURLToPath(import.meta.url));
const rendererDir = join(here, "..", "src", "renderer");

/** Çevrilmeyen, adıyla izin verilen metinler: marka ve ayraçlar. */
const ALLOWED = new Set(["KΛDRΛN"]);
const VISIBLE_ATTRS = new Set(["title", "aria-label", "placeholder", "alt"]);
const LETTER = /\p{L}/u;

async function tsxFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "icons") out.push(...(await tsxFiles(path)));
    } else if (entry.name.endsWith(".tsx")) {
      out.push(path);
    }
  }
  return out;
}

export async function rawTexts(): Promise<{ files: number; problems: string[] }> {
  const problems: string[] = [];
  const files = await tsxFiles(rendererDir);
  for (const file of files) {
    const source = ts.createSourceFile(file, await readFile(file, "utf8"), ts.ScriptTarget.Latest, true, ts.ScriptKind.TSX);
    const visit = (node: ts.Node): void => {
      if (ts.isJsxText(node)) {
        const text = node.text.trim();
        if (text && LETTER.test(text) && !ALLOWED.has(text)) problems.push(`${file}: "${text}"`);
      }
      if (ts.isJsxAttribute(node) && VISIBLE_ATTRS.has(node.name.getText(source))) {
        const init = node.initializer;
        if (init && ts.isStringLiteral(init) && LETTER.test(init.text) && !ALLOWED.has(init.text)) {
          problems.push(`${file}: ${node.name.getText(source)}="${init.text}"`);
        }
      }
      ts.forEachChild(node, visit);
    };
    visit(source);
  }
  return { files: files.length, problems };
}

test("JSX'te çeviri dışı görünür metin yok", async () => {
  const { files, problems } = await rawTexts();
  assert.ok(files >= 15, "hiç .tsx dosyası taranmadı — yol kaymış olabilir");
  assert.deepEqual(problems, []);
});

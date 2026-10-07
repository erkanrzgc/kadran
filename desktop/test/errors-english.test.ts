/**
 * Ana süreçte ve paylaşılan kodda `new …Error(…)` iletileri İngilizce:
 * IPC üzerinden arayüze, oradan bildirime ulaşıyorlar (K-141). Konsol
 * tanılamaları (onWarning, onDiagnostic) bu kurala tabi değil.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import ts from "typescript";

const here = dirname(fileURLToPath(import.meta.url));
const roots = [join(here, "..", "src", "main"), join(here, "..", "src", "shared")];
const TURKISH = /[çğıöşüÇĞİÖŞÜ]/;

async function tsFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const e of await readdir(dir, { withFileTypes: true })) {
    if (e.isDirectory()) out.push(...(await tsFiles(join(dir, e.name))));
    else if (e.name.endsWith(".ts")) out.push(join(dir, e.name));
  }
  return out;
}

test("Error iletileri İngilizce", async () => {
  const problems: string[] = [];
  let errors = 0;
  for (const root of roots) {
    for (const file of await tsFiles(root)) {
      const sf = ts.createSourceFile(file, await readFile(file, "utf8"), ts.ScriptTarget.Latest, true);
      const visit = (node: ts.Node): void => {
        if (ts.isNewExpression(node) && /Error$/.test(node.expression.getText(sf))) {
          errors++;
          const texts: string[] = [];
          const collect = (n: ts.Node): void => {
            if (ts.isStringLiteral(n) || ts.isNoSubstitutionTemplateLiteral(n)) texts.push(n.text);
            if (ts.isTemplateExpression(n)) {
              texts.push(n.head.text, ...n.templateSpans.map((s) => s.literal.text));
            }
            ts.forEachChild(n, collect);
          };
          for (const arg of node.arguments ?? []) collect(arg);
          for (const text of texts) if (TURKISH.test(text)) problems.push(`${file}: ${text}`);
        }
        ts.forEachChild(node, visit);
      };
      visit(sf);
    }
  }
  assert.ok(errors >= 8, `yalnız ${errors} hata ifadesi bulundu — yollar kaymış olabilir`);
  assert.deepEqual(problems, []);
});

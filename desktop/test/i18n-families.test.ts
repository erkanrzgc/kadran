/**
 * Şablonlu t(`…${x}…`) çağrıları: i18n.test.ts yalnızca sabit t("…") anahtarlarını
 * görür. Burada her şablonlu çağrı bir "aile"ye bağlanır, ailenin olası üyeleri
 * kaynaktaki sabit dizilerden / proto sıralamalarından çıkarılır ve en+tr'de var
 * olduğu doğrulanır. Kaynakta yeni bir şablonlu çağrı belirir ama burada aile
 * tanımlanmazsa test düşer (ve tersi: kaynakta artık olmayan aile de düşer).
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import { ACCENTS, ALL_MODES, TONES } from "../src/shared/appearance.ts";
import { OUTCOME_KEYS } from "../src/renderer/lib/chain.ts";

const here = dirname(fileURLToPath(import.meta.url));
const rendererDir = join(here, "..", "src", "renderer");
const protoPath = join(here, "..", "..", "proto", "kadran", "v1", "api.proto");

async function sourceFiles(dir: string): Promise<string[]> {
  const out: string[] = [];
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const path = join(dir, entry.name);
    if (entry.isDirectory()) {
      if (entry.name !== "icons") out.push(...(await sourceFiles(path)));
    } else if (/\.(ts|tsx)$/.test(entry.name)) out.push(path);
  }
  return out;
}

const dict = async (lang: string) => JSON.parse(await readFile(join(rendererDir, "i18n", `${lang}.json`), "utf8")) as Record<string, string>;

/** proto dosyasındaki bir enum'un değer adları. */
function protoEnum(proto: string, name: string): string[] {
  const body = proto.match(new RegExp(`enum ${name} \{([^}]*)\}`))?.[1];
  assert.ok(body, `proto: ${name} bulunamadı`);
  return [...body.matchAll(/^\s*([A-Z0-9_]+)\s*=/gm)].map((m) => m[1]!);
}

/** AppsScreen'deki sekme dizisi: `(["a", "b"] as const).map((name) => … apps.tab_${name}`. */
function appTabs(src: string): string[] {
  const list = src.match(/\(\[([^\]]+)\] as const\)\.map\(\(name\)/)?.[1];
  assert.ok(list, "AppsScreen: sekme dizisi bulunamadı");
  return [...list.matchAll(/"([a-z_]+)"/g)].map((m) => m[1]!);
}

test("şablonlu t(`…`) çağrılarının her ailesi tanımlı ve üyeleri en+tr'de var", async () => {
  const proto = await readFile(protoPath, "utf8");
  const appsSrc = await readFile(join(rendererDir, "screens", "AppsScreen.tsx"), "utf8");
  const actions = ["deploy", "rollback", "delete"]; // OperationDialog.tsx AppAction

  const families: Record<string, readonly string[]> = {
    "operation.${}_title": actions,
    "operation.${}_body": actions,
    "action.${}": actions,
    "outcome.${}": [...OUTCOME_KEYS, "unspecified"],
    "alarm.${}": protoEnum(proto, "AlarmSeverity"),
    "release.${}": protoEnum(proto, "ReleaseStatus"),
    "apps.tab_${}": appTabs(appsSrc),
    "mode.${}": ALL_MODES,
    "accent.${}": ACCENTS,
    "tone.${}": TONES,
  };

  const found = new Set<string>();
  for (const file of await sourceFiles(rendererDir)) {
    const src = await readFile(file, "utf8");
    for (const m of src.matchAll(/\bt\(\s*`([^`]*)`/g)) {
      if (m[1]!.includes("${")) found.add(m[1]!.replace(/\$\{[^}]*\}/g, "${}"));
    }
  }

  assert.deepEqual([...found].filter((p) => !(p in families)).sort(), [], "ailesi tanımlanmamış şablonlu t(`…`) çağrısı");
  assert.deepEqual(Object.keys(families).filter((p) => !found.has(p)).sort(), [], "kaynakta artık olmayan aile");

  const en = await dict("en");
  const tr = await dict("tr");
  for (const [pattern, members] of Object.entries(families)) {
    assert.ok(members.length > 0, `${pattern}: üye çıkarılamadı`);
    for (const member of members) {
      const key = pattern.replace("${}", member);
      assert.ok(key in en, `en.json'da yok: ${key}`);
      assert.ok(key in tr, `tr.json'da yok: ${key}`);
    }
  }
});

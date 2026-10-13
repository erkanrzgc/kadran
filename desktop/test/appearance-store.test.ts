/**
 * appearance.json: yoksa varsayılan, bozuksa varsayılan + uyarı, yazım
 * atomik (geçici dosya + yeniden adlandırma); başarısız yazımda geçici
 * dosya kalmaz.
 */

import { test } from "node:test"
import assert from "node:assert/strict"
import { mkdir, mkdtemp, readdir, readFile, rm, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"

import { appearancePath, loadAppearance, saveAppearance } from "../src/main/appearance.ts"
import { defaultAppearance } from "../src/shared/appearance.ts"

const WIN11 = { platform: "win32", release: "10.0.26200" }

async function tempDir(): Promise<string> {
  return mkdtemp(join(tmpdir(), "kadran-gorunum-"))
}

test("dosya yoksa varsayılan, uyarı yok", async () => {
  const dir = await tempDir()
  try {
    const warnings: string[] = []
    assert.deepEqual(await loadAppearance(dir, WIN11, (m) => warnings.push(m)), defaultAppearance(WIN11))
    assert.deepEqual(warnings, [])
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

test("bozuk JSON varsayılana döner ve uyarır", async () => {
  const dir = await tempDir()
  try {
    await writeFile(appearancePath(dir), "{ bu json değil", "utf8")
    const warnings: string[] = []
    assert.deepEqual(await loadAppearance(dir, WIN11, (m) => warnings.push(m)), defaultAppearance(WIN11))
    assert.equal(warnings.length, 1)
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

test("kaydet → oku aynı değeri verir; değer temizlenerek yazılır", async () => {
  const dir = await tempDir()
  try {
    const saved = await saveAppearance(dir, { tint: 48, panel: 99, mode: "mica", extra: "atılır" }, WIN11)
    assert.equal(saved.tint, 48)
    assert.equal(saved.panel, 30)
    assert.equal(saved.mode, "mica")
    assert.deepEqual(await loadAppearance(dir, WIN11), saved)
    const onDisk = JSON.parse(await readFile(appearancePath(dir), "utf8")) as Record<string, unknown>
    assert.equal("extra" in onDisk, false)
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

test("var olan dosyanın üstüne yazılır ve geçici dosya kalmaz", async () => {
  const dir = await tempDir()
  try {
    await saveAppearance(dir, { tint: 10 }, WIN11)
    await saveAppearance(dir, { tint: 20 }, WIN11)
    assert.equal((await loadAppearance(dir, WIN11)).tint, 20)
    assert.deepEqual((await readdir(dir)).sort(), ["appearance.json"])
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

test("yazım başarısızsa hata fırlar, geçici dosya silinir", async () => {
  const dir = await tempDir()
  try {
    // Hedef bir DİZİN: yeniden adlandırma başarısız olur.
    await mkdir(appearancePath(dir))
    await assert.rejects(saveAppearance(dir, { tint: 10 }, WIN11))
    assert.deepEqual(await readdir(dir), ["appearance.json"])
  } finally {
    await rm(dir, { recursive: true, force: true })
  }
})

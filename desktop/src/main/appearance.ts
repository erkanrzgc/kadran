/**
 * Görünüm ayarlarının kalıcı saklanması: profiles.json'ın yanında
 * appearance.json.
 *
 * Yazım ATOMİK: önce geçici dosyaya, sonra yeniden adlandırma. Yarıda
 * kesilen bir yazım (güç kesintisi, çökme) yarım bir JSON bırakmaz; en
 * kötü durumda eski dosya kalır. (profiles.ts bugün düz writeFile
 * kullanıyor; bu desen yeni ve ona dokunulmadı.)
 *
 * Okunan her değer sanitizeAppearance'tan geçer: dosya elle düzenlenmiş
 * ya da bozulmuş olabilir.
 */

import { mkdir, readFile, rename, rm, writeFile } from "node:fs/promises"
import { dirname, join } from "node:path"

import { sanitizeAppearance, type Appearance, type PlatformInfo } from "../shared/appearance.ts"

export function appearancePath(userDataDir: string): string {
  return join(userDataDir, "appearance.json")
}

export async function loadAppearance(
  userDataDir: string,
  info: PlatformInfo,
  onWarning?: (message: string) => void,
): Promise<Appearance> {
  let raw: string
  try {
    raw = await readFile(appearancePath(userDataDir), "utf8")
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
      onWarning?.(`görünüm ayarları okunamadı: ${(err as Error).message}`)
    }
    return sanitizeAppearance(null, info)
  }
  try {
    return sanitizeAppearance(JSON.parse(raw) as unknown, info)
  } catch (err) {
    onWarning?.(`görünüm dosyası çözümlenemedi, varsayılan kullanılıyor: ${(err as Error).message}`)
    return sanitizeAppearance(null, info)
  }
}

export async function saveAppearance(userDataDir: string, raw: unknown, info: PlatformInfo): Promise<Appearance> {
  const clean = sanitizeAppearance(raw, info)
  const path = appearancePath(userDataDir)
  await mkdir(dirname(path), { recursive: true })
  const tmp = `${path}.${process.pid}.${Date.now()}.tmp`
  try {
    await writeFile(tmp, JSON.stringify(clean, null, 2) + "\n", "utf8")
    await rename(tmp, path)
  } catch (err) {
    await rm(tmp, { force: true })
    throw err
  }
  return clean
}

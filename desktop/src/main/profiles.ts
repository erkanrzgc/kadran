/**
 * Sunucu profillerinin kalıcı saklanması.
 *
 * # Neden işletim sistemi anahtarlığı DEĞİL?
 *
 * Plan profilleri OS anahtarlığında saklamayı öngörüyordu. O madde bu
 * tasarımda gereksiz: bir profil `panely-client@1.2.3.4` gibi bir hedef
 * dizesinden ibaret ve İÇİNDE SIR YOK. Kimlik doğrulamayı `ssh` yapıyor,
 * anahtar ssh-agent'ta ya da ~/.ssh altında duruyor; bu uygulama anahtar
 * malzemesini hiç görmüyor.
 *
 * Anahtarlık kullanmak sıfır kazanç karşılığında bir arıza kipi eklerdi:
 * anahtarlık sıfırlandığında çözülemeyen bir bloba dönüşen profiller.
 * Düz JSON, kullanıcının okuyup düzenleyebileceği bir dosya olarak daha
 * dürüst.
 */

import { readFile, writeFile, mkdir } from "node:fs/promises";
import { dirname, join } from "node:path";

/** Profil, kayıtlı bir bağlantı hedefidir. */
export interface Profile {
  /** Kullanıcının verdiği ad. */
  name: string;
  /** `panely` hedef dizesi: kullanici@sunucu[:port] veya unix:// yolu. */
  target: string;
}

/** MAX_PROFILES, saklanabilecek profil sayısının üst sınırı. */
const MAX_PROFILES = 100;

export function profilesPath(userDataDir: string): string {
  return join(userDataDir, "profiles.json");
}

/**
 * loadProfiles, kayıtlı profilleri okur.
 *
 * Dosya yoksa boş liste döner — ilk çalıştırma bir hata değil. Dosya
 * bozuksa da boş liste döner ve nedeni bildirir: kullanıcıyı açılışta
 * çözemeyeceği bir hatayla karşılamaktansa temiz başlamak iyi.
 */
export async function loadProfiles(
  userDataDir: string,
  onWarning?: (message: string) => void,
): Promise<Profile[]> {
  let raw: string;
  try {
    raw = await readFile(profilesPath(userDataDir), "utf8");
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code === "ENOENT") return [];
    onWarning?.(`profiller okunamadı: ${(err as Error).message}`);
    return [];
  }

  try {
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) {
      onWarning?.("profil dosyası bir dizi değil, yok sayıldı");
      return [];
    }
    return parsed.filter(isProfile).slice(0, MAX_PROFILES);
  } catch (err) {
    onWarning?.(`profil dosyası çözümlenemedi: ${(err as Error).message}`);
    return [];
  }
}

/** saveProfiles, profilleri diske yazar. */
export async function saveProfiles(
  userDataDir: string,
  profiles: unknown,
): Promise<Profile[]> {
  if (!Array.isArray(profiles)) {
    throw new Error("profiller bir dizi olmalı");
  }
  // Renderer'dan gelen veri DOĞRULANIR. Güvenilmez bir kaynaktan gelen
  // yapıyı olduğu gibi diske yazmak, bir sonraki okumada beklenmedik
  // şekiller üretir.
  const clean = profiles.filter(isProfile).slice(0, MAX_PROFILES);

  const path = profilesPath(userDataDir);
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, JSON.stringify(clean, null, 2) + "\n", "utf8");
  return clean;
}

function isProfile(value: unknown): value is Profile {
  if (typeof value !== "object" || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.name === "string" &&
    v.name.length > 0 &&
    v.name.length <= 200 &&
    typeof v.target === "string" &&
    v.target.length > 0 &&
    v.target.length <= 500
  );
}

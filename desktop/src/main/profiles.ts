/**
 * Sunucu profillerinin kalıcı saklanması.
 *
 * # Neden işletim sistemi anahtarlığı DEĞİL?
 *
 * Plan profilleri OS anahtarlığında saklamayı öngörüyordu. O madde bu
 * tasarımda gereksiz: bir profil `kadran-client@1.2.3.4` gibi bir hedef
 * dizesinden ibaret ve İÇİNDE SIR YOK. Kimlik doğrulamayı `ssh` yapıyor,
 * anahtar ssh-agent'ta ya da ~/.ssh altında duruyor; bu uygulama anahtar
 * malzemesini hiç görmüyor.
 *
 * Anahtarlık kullanmak sıfır kazanç karşılığında bir arıza kipi eklerdi:
 * anahtarlık sıfırlandığında çözülemeyen bir bloba dönüşen profiller.
 * Düz JSON, kullanıcının okuyup düzenleyebileceği bir dosya olarak daha
 * dürüst.
 */

import { readFile, writeFile, mkdir, rename, rm } from "node:fs/promises";
import { randomBytes } from "node:crypto";
import { dirname, join } from "node:path";

/** Profil, kayıtlı bir bağlantı hedefidir. */
export interface Profile {
  /** Kullanıcının verdiği ad. */
  name: string;
  /** `kadran` hedef dizesi: kullanici@sunucu[:port] veya unix:// yolu. */
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

/** Dosya işlemleri; testte yarıda kesilen yazımı taklit etmek için değiştirilebilir. */
export interface FileOps {
  writeFile(path: string, data: string): Promise<void>;
  rename(from: string, to: string): Promise<void>;
  rm(path: string): Promise<void>;
}

const realOps: FileOps = {
  writeFile: (path, data) => writeFile(path, data, "utf8"),
  rename,
  rm: (path) => rm(path, { force: true }),
};

/**
 * saveProfiles, profilleri diske ATOMİK yazar: aynı dizindeki geçici dosyaya
 * yazıp `rename` ile yerine koyar. Yazım yarıda kesilirse (disk dolu, süreç
 * ölümü) eski profil dosyası sağlam kalır; hata durumunda geçici dosya silinir.
 */
export async function saveProfiles(
  userDataDir: string,
  profiles: unknown,
  ops: FileOps = realOps,
): Promise<Profile[]> {
  if (!Array.isArray(profiles)) {
    throw new Error("profiles must be a list");
  }
  // Renderer'dan gelen veri DOĞRULANIR. Güvenilmez bir kaynaktan gelen
  // yapıyı olduğu gibi diske yazmak, bir sonraki okumada beklenmedik
  // şekiller üretir.
  const clean = profiles.filter(isProfile).slice(0, MAX_PROFILES);

  const path = profilesPath(userDataDir);
  await mkdir(dirname(path), { recursive: true });
  const temp = `${path}.${randomBytes(6).toString("hex")}.tmp`;
  try {
    await ops.writeFile(temp, JSON.stringify(clean, null, 2) + "\n");
    await ops.rename(temp, path);
  } catch (err) {
    await ops.rm(temp).catch(() => undefined);
    throw err;
  }
  return clean;
}

/** Göçten önceki istemci kullanıcısı; sunucu göçü onu `kadran-client` yapar. */
const LEGACY_CLIENT = "panely-client@";
const CLIENT = "kadran-client@";

/**
 * migrateLegacyProfiles, eski adlı uygulamanın (`panely-desktop`)
 * profillerini yeni kullanıcı verisi dizinine taşır (K-136).
 *
 * Electron dizini paket adından türetiyor; ad değişince profiller eski
 * dizinde kalır ve uygulama boş açılırdı. Hedeflerdeki `panely-client@`
 * de `kadran-client@` olur: sunucu göçünden sonra eski kullanıcı yok.
 * Başka kullanıcı adlarına dokunulmaz.
 *
 * Yeni dizinde profil dosyası VARSA hiçbir şey yapılmaz (kullanıcının yeni
 * profillerinin üstüne yazılmaz). Eski dosya silinmez. Taşınan profil
 * sayısını döner.
 */
export async function migrateLegacyProfiles(
  userDataDir: string,
  legacyDir: string,
  onWarning?: (message: string) => void,
): Promise<number> {
  try {
    await readFile(profilesPath(userDataDir), "utf8");
    return 0;
  } catch (err) {
    if ((err as NodeJS.ErrnoException).code !== "ENOENT") {
      onWarning?.(`profiller okunamadı: ${(err as Error).message}`);
      return 0;
    }
  }

  let warned = false;
  const legacy = await loadProfiles(legacyDir, (msg) => {
    warned = true;
    onWarning?.(`eski profiller taşınamadı: ${msg}`);
  });
  if (warned || legacy.length === 0) return 0;

  const moved = legacy.map((p) =>
    p.target.startsWith(LEGACY_CLIENT)
      ? { ...p, target: CLIENT + p.target.slice(LEGACY_CLIENT.length) }
      : p,
  );
  const saved = await saveProfiles(userDataDir, moved);
  return saved.length;
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

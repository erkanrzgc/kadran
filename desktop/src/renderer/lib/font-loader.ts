/**
 * Tarayıcı tarafı: kurulu yazı tiplerini listeler ve bir aileyi bir takma
 * adla (kadran-ui / kadran-code) kaydeder.
 *
 * Ölçüldü (Electron 43): izin reddedilirse queryLocalFonts() hata değil
 * BOŞ liste döner. İkisi de "listelenemedi" demek; arayüz bunu gösterir.
 *
 * Eşzamanlı kayıtlar aynı takma ad için yarışabilir. Son çağrı kazanır; eski
 * yüzler kaldırılır ve stale sonuçlar atılır.
 */

import { cssLocalSource, type Face, type LocalFontLike } from "./fonts.ts";

type QueryLocalFonts = () => Promise<LocalFontLike[]>;

export async function listLocalFonts(): Promise<LocalFontLike[]> {
  const query = (window as unknown as { queryLocalFonts?: QueryLocalFonts }).queryLocalFonts;
  if (typeof query !== "function") return [];
  try {
    return await query();
  } catch {
    return [];
  }
}

const registered = new Map<string, FontFace[]>();
const generations = new Map<string, number>();

/**
 * registerFamily, eski kaydı siler, yüzleri yükler; yüklenen yüz sayısını verir.
 * Eşzamanlı çağrılar aynı takma ad için yarışırsa, son çağrı kazanır; eski
 * çağrının sonuçları atılır (0 döner).
 */
export async function registerFamily(alias: string, faces: readonly Face[]): Promise<number> {
  // Her çağrı bir nesil alır; yeni nesil eski çağrıları geçersiz kılar.
  const gen = (generations.get(alias) ?? 0) + 1;
  generations.set(alias, gen);

  // Tüm yüzleri yerel diziye yükle; henüz document.fonts'a ekleme.
  const loaded: FontFace[] = [];
  for (const face of faces) {
    const ff = new FontFace(alias, cssLocalSource(face.postscriptName), {
      weight: String(face.weight),
      style: face.italic ? "italic" : "normal",
    });
    try {
      await ff.load();
      loaded.push(ff);
    } catch {
      // Yüklenemeyen yüz atlanır; ailenin diğer yüzleri yeterli olabilir.
    }
  }

  // Eğer bu çağrı arasında yeni bir çağrı başladıysa, bu sonuçlar stale.
  if (generations.get(alias) !== gen) {
    return 0;
  }

  // Atomik takas: eski yüzleri sil, yenilerini ekle.
  for (const old of registered.get(alias) ?? []) {
    document.fonts.delete(old);
  }
  for (const ff of loaded) {
    document.fonts.add(ff);
  }
  registered.set(alias, loaded);
  return loaded.length;
}

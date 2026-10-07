/**
 * Tarayıcı tarafı: kurulu yazı tiplerini listeler ve bir aileyi bir takma
 * adla (kadran-ui / kadran-code) kaydeder.
 *
 * Ölçüldü (Electron 43): izin reddedilirse queryLocalFonts() hata değil
 * BOŞ liste döner. İkisi de "listelenemedi" demek; arayüz bunu gösterir.
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

/** registerFamily, eski kaydı siler, yüzleri yükler; yüklenen yüz sayısını verir. */
export async function registerFamily(alias: string, faces: readonly Face[]): Promise<number> {
  for (const old of registered.get(alias) ?? []) document.fonts.delete(old);
  const added: FontFace[] = [];
  for (const face of faces) {
    const ff = new FontFace(alias, cssLocalSource(face.postscriptName), {
      weight: String(face.weight),
      style: face.italic ? "italic" : "normal",
    });
    try {
      await ff.load();
      document.fonts.add(ff);
      added.push(ff);
    } catch {
      // Yüklenemeyen yüz atlanır; ailenin diğer yüzleri yeterli olabilir.
    }
  }
  registered.set(alias, added);
  return added.length;
}

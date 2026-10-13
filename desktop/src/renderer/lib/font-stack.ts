/**
 * Bir takma ad için "kaydet, sonra yığını seç" adımı. registerFamily'yi
 * parametre olarak alır, böylece DOM olmadan ölçülür.
 *
 * Her takma adı tek bir etki çağırır; bu yüzden CANLI (iptal edilmemiş)
 * bir çağrının 0 dönmesi gerçek başarısızlıktır. İptal edilmiş çağrı
 * null döner ve durumu ASLA ezmez.
 */

import { fontStack, type FontAlias } from "./apply-appearance.ts";
import type { Face } from "./fonts.ts";

export type Register = (alias: string, faces: readonly Face[]) => Promise<number>;

export async function resolveStack(
  alias: FontAlias,
  faces: readonly Face[],
  fallback: string,
  register: Register,
  isCancelled: () => boolean,
): Promise<string | null> {
  let ok = false;
  try {
    ok = faces.length > 0 && (await register(alias, faces)) > 0;
  } catch {
    // Kayıt patladı: görünür sonuç yedek yığın; kullanıcı sistem yazı tipini görür.
    ok = false;
  }
  if (isCancelled()) return null;
  return fontStack(ok ? alias : null, fallback);
}

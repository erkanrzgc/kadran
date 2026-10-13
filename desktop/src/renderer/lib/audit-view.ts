/**
 * Kayıt tablolarının (Status önizlemesi, Audit ekranı) ortak görünüm kuralları.
 */

export interface PlaceholderInput {
  /** Yüklü kayıt sayısı. */
  total: number;
  /** Süzgeçten geçen kayıt sayısı. */
  shown: number;
  failed: boolean;
  loaded: boolean;
}

/**
 * Tablonun boş satırının i18n anahtarı; satır gerekmiyorsa undefined.
 * Yüklenemeyen liste "kayıt yok" diye gösterilmez (fail-closed); süzgecin
 * hepsini elemesi "zincir boş" değildir.
 */
export function auditPlaceholderKey({ total, shown, failed, loaded }: PlaceholderInput): string | undefined {
  if (total > 0) return shown === 0 ? "audit.no_match" : undefined;
  if (failed) return "audit.load_failed";
  return loaded ? "audit.empty" : "audit.loading";
}

/**
 * effectiveChoice: seçilen değer artık seçenekler arasında yoksa (aralık
 * küçüldü, yenilendi) "" (tümü) döner. Seçici "All" gösterirken süzgecin
 * kayıtları gizlemesi önlenir: ikisi aynı değeri kullanır.
 */
export function effectiveChoice(selected: string, available: readonly string[]): string {
  return available.includes(selected) ? selected : "";
}

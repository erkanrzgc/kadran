/**
 * Kullanıcı ayarlarının biçimi: dil, tema ve yazı tipleri. Tema iki tanedir:
 * açık ve koyu. Cam ayrı bir tema değil, her temada çerçevede (K-163); ton ya
 * da vurgu ayarı yoktur.
 *
 * Doğrulama, varsayılanlar ve dosyaya yazma Go'da
 * (desktop/internal/appearance, K-165); arayüz ayarları oradan alır ve
 * oraya gönderir. Buradakiler yalnız arayüzün türleri ve listeleri.
 */

export type Language = "en" | "tr";
export type Theme = "dark" | "light";

export interface FontChoice {
  /** Boş dize: gömülü Geist. */
  family: string;
  weight: number;
}

export const APPEARANCE_VERSION = 2;

export interface Appearance {
  version: typeof APPEARANCE_VERSION;
  language: Language;
  theme: Theme;
  uiFont: FontChoice;
  codeFont: FontChoice;
}

/**
 * Pencerenin yerel malzemesi; arkada masaüstü bulanık görünür. `none`: opak
 * çerçeve. `accent`: Windows camı, odak gidince de kalır. `acrylic`: sistemin
 * malzemesi, cam uygulanamazsa; odak gidince griye düşer.
 */
export type Material = "accent" | "acrylic" | "none";

export const LANGUAGE_CODES: readonly Language[] = ["en", "tr"];
export const THEMES: readonly Theme[] = ["dark", "light"];

/** Go'daki appearance.Defaults ile aynı; testlerin başlangıç değeri. */
export function defaultAppearance(): Appearance {
  return {
    version: APPEARANCE_VERSION,
    language: "en",
    theme: "light",
    uiFont: { family: "", weight: 400 },
    codeFont: { family: "", weight: 400 },
  };
}

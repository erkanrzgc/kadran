/**
 * Kurulu yazı tipleri. queryLocalFonts() her YÜZÜ ayrı verir (aile, stil,
 * PostScript adı). Seçilen ailenin her yüzü kendi kalınlığıyla
 * kaydedilir; sahte kalınlaştırma kapalı (font-synthesis: none). Böylece
 * JetBrainsMono Nerd Font yalnız Medium olarak kuruluysa Medium çizilir.
 */

export interface LocalFontLike {
  family: string;
  style: string;
  postscriptName: string;
}

export interface Face extends LocalFontLike {
  weight: number;
  italic: boolean;
}

const WEIGHTS: Readonly<Record<string, number>> = {
  thin: 100,
  hairline: 100,
  extralight: 200,
  ultralight: 200,
  light: 300,
  semilight: 350,
  regular: 400,
  normal: 400,
  book: 400,
  retina: 450,
  medium: 500,
  semibold: 600,
  demibold: 600,
  bold: 700,
  extrabold: 800,
  ultrabold: 800,
  black: 900,
  heavy: 900,
};

export function parseStyle(style: string): { weight: number; italic: boolean } {
  const s = style.toLowerCase().replace(/[\s_-]/g, "");
  const italic = /italic|oblique/.test(s);
  const core = s.replace(/italic|oblique/g, "") || "regular";
  return { weight: WEIGHTS[core] ?? 400, italic };
}

export function groupFaces(list: readonly LocalFontLike[]): Map<string, Face[]> {
  const map = new Map<string, Face[]>();
  for (const f of list) {
    const face: Face = { ...f, ...parseStyle(f.style) };
    map.set(f.family, [...(map.get(f.family) ?? []), face]);
  }
  return map;
}

/** weightsOf, ailedeki italik olmayan kalınlıklar; aile boşsa [400]. */
export function weightsOf(faces: readonly Face[]): number[] {
  const set = new Set(faces.filter((f) => !f.italic).map((f) => f.weight));
  return set.size === 0 ? [400] : [...set].sort((a, b) => a - b);
}

export function nearestWeight(faces: readonly Face[], wanted: number): number {
  if (faces.length === 0) return wanted;
  return weightsOf(faces).reduce((best, w) => (Math.abs(w - wanted) < Math.abs(best - wanted) ? w : best));
}

/** strongerWeight, başlıklar için bir üst kalınlık; yoksa aynısı. */
export function strongerWeight(faces: readonly Face[], base: number): number {
  return weightsOf(faces).find((w) => w > base) ?? base;
}

export function cssLocalSource(postscriptName: string): string {
  return `local("${postscriptName.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}")`;
}

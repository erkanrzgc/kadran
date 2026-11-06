/**
 * Kullanıcı ayarları: dil, tema ve yazı tipleri. Tema iki tanedir: açık ve
 * koyu. Cam ayrı bir tema değil, her temada çerçevede (kenar çubuğu, üst
 * çubuk; K-163); ton ya da vurgu ayarı yoktur. Ana süreç (doğrulayıp diske
 * yazmak için) ve arayüz aynı dosyayı kullanır; Node ya da DOM'a bağımlı değil.
 *
 * Dosya elle düzenlenebilir ya da bozulabilir. Her alan TEK TEK
 * doğrulanır; geçersiz alan varsayılana döner, geçerli alanlar korunur.
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

export interface PlatformInfo {
  platform: string;
  /** os.release(): Windows'ta "10.0.<yapı>". */
  release: string;
}

/**
 * Pencerenin yerel malzemesi; arkada masaüstü bulanık görünür. `none`: opak koyu.
 * `accent`: Windows camı, odak gidince de kalır (main/window-accent.ts).
 * `acrylic`: Electron'un malzemesi, accent yüklenemezse; odak gidince griye düşer.
 */
export type Material = "accent" | "acrylic" | "vibrancy" | "none";

/** Pencerenin yerel tarafı; arayüz renkleri styles/tokens.css'te. */
export interface WindowLook {
  material: Material;
  background: string;
  scheme: "dark" | "light";
}

export const LANGUAGE_CODES: readonly Language[] = ["en", "tr"];
export const THEMES: readonly Theme[] = ["dark", "light"];

/** Yerel malzeme yokken çerçevenin opak rengi; tokens.css'teki --frame-solid ile aynı. */
const SOLID_FRAME: Record<Theme, string> = { dark: "#0a0a0a", light: "#ededf0" };

export const LIMITS = {
  weight: [100, 900],
} as const satisfies Record<string, readonly [number, number]>;

export const MAX_FONT_FAMILY = 200;

/** Acrylic (backgroundMaterial) Windows 11 22H2 (yapı 22621) ister. */
const WIN11_BUILD = 22621;

export function isWindows11(info: PlatformInfo): boolean {
  if (info.platform !== "win32") return false;
  const build = Number(info.release.split(".")[2]);
  return Number.isFinite(build) && build >= WIN11_BUILD;
}

export function materialFor(info: PlatformInfo): Material {
  if (isWindows11(info)) return "acrylic";
  if (info.platform === "darwin") return "vibrancy";
  return "none";
}

/**
 * Cam her temada çerçevede: malzeme sistemin, temadan bağımsız. Malzemesi
 * olmayan sistemde pencere çerçevenin opak rengiyle açılır.
 */
export function windowLook(theme: Theme, available: Material): WindowLook {
  if (available === "none") return { material: "none", background: SOLID_FRAME[theme], scheme: theme };
  return { material: available, background: "#00000000", scheme: theme };
}

export function defaultAppearance(): Appearance {
  return {
    version: APPEARANCE_VERSION,
    language: "en",
    theme: "light",
    uiFont: { family: "", weight: 400 },
    codeFont: { family: "", weight: 400 },
  };
}

function clampInt(value: unknown, [lo, hi]: readonly [number, number], fallback: number): number {
  if (typeof value !== "number" || !Number.isFinite(value)) return fallback;
  return Math.min(hi, Math.max(lo, Math.round(value)));
}

function pick<T extends string>(value: unknown, allowed: readonly T[], fallback: T): T {
  return typeof value === "string" && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

function font(value: unknown, fallback: FontChoice): FontChoice {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return fallback;
  const o = value as Record<string, unknown>;
  const family =
    typeof o.family === "string" && o.family.length <= MAX_FONT_FAMILY ? o.family : fallback.family;
  return { family, weight: clampInt(o.weight, LIMITS.weight, fallback.weight) };
}

/**
 * sanitizeAppearance, güvenilmez bir değerden geçerli bir Appearance üretir.
 * Eski sürümün kayıtlı yazı tipi yeni görünümü bozmasın: yalnız dil taşınır.
 * Kaldırılan cam teması ("glass") bilinmeyen değer gibi varsayılana (açık) döner.
 */
export function sanitizeAppearance(raw: unknown): Appearance {
  const d = defaultAppearance();
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return d;
  const o = raw as Record<string, unknown>;
  const language = pick(o.language, LANGUAGE_CODES, d.language);
  if (o.version !== APPEARANCE_VERSION) return { ...d, language };
  return { version: APPEARANCE_VERSION, language, theme: pick(o.theme, THEMES, d.theme), uiFont: font(o.uiFont, d.uiFont), codeFont: font(o.codeFont, d.codeFont) };
}

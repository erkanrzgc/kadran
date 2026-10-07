/**
 * Görünüm ayarları: şema, işletim sistemine göre modlar, varsayılanlar ve
 * temizleme. Ana süreç (doğrulayıp diske yazmak için) ve arayüz (Reset ve
 * seçenekler için) aynı dosyayı kullanır; Node ya da DOM'a bağımlı değil.
 *
 * Dosya elle düzenlenebilir ya da bozulabilir. Her alan TEK TEK
 * doğrulanır; geçersiz alan varsayılana döner, geçerli alanlar korunur.
 */

export type WindowMode = "clear" | "acrylic" | "mica" | "vibrancy" | "solid";
export type Accent = "ice" | "amber" | "phosphor" | "paper";
export type Tone = "dark" | "light";
export type Language = "en" | "tr";

export interface FontChoice {
  /** Boş dize: arayüzde sistem yazı tipi, kodda gömülü JetBrains Mono. */
  family: string;
  weight: number;
}

export interface Appearance {
  version: 1;
  mode: WindowMode;
  tint: number;
  panel: number;
  edge: number;
  glow: number;
  accent: Accent;
  tone: Tone;
  language: Language;
  uiFont: FontChoice;
  codeFont: FontChoice;
}

export interface PlatformInfo {
  platform: string;
  /** os.release(): Windows'ta "10.0.<yapı>". */
  release: string;
}

export const ALL_MODES: readonly WindowMode[] = ["clear", "acrylic", "mica", "vibrancy", "solid"];
export const ACCENTS: readonly Accent[] = ["ice", "amber", "phosphor", "paper"];
export const TONES: readonly Tone[] = ["dark", "light"];
export const LANGUAGE_CODES: readonly Language[] = ["en", "tr"];

export const LIMITS = {
  tint: [0, 100],
  panel: [0, 30],
  edge: [0, 60],
  glow: [0, 100],
  weight: [100, 900],
} as const satisfies Record<string, readonly [number, number]>;

export const MAX_FONT_FAMILY = 200;

/** Acrylic ve Mica (backgroundMaterial) Windows 11 22H2 (yapı 22621) ister. */
const WIN11_BUILD = 22621;

export function isWindows11(info: PlatformInfo): boolean {
  if (info.platform !== "win32") return false;
  const build = Number(info.release.split(".")[2]);
  return Number.isFinite(build) && build >= WIN11_BUILD;
}

export function modesFor(info: PlatformInfo): readonly WindowMode[] {
  if (info.platform === "win32") return isWindows11(info) ? ["clear", "acrylic", "mica", "solid"] : ["clear", "solid"];
  if (info.platform === "darwin") return ["clear", "vibrancy", "solid"];
  return ["clear", "solid"];
}

export function defaultMode(info: PlatformInfo): WindowMode {
  if (isWindows11(info)) return "acrylic";
  if (info.platform === "win32") return "clear";
  if (info.platform === "darwin") return "vibrancy";
  return "solid";
}

export function defaultAppearance(info: PlatformInfo): Appearance {
  return {
    version: 1,
    mode: defaultMode(info),
    tint: 60,
    panel: 7,
    edge: 22,
    glow: 60,
    accent: "ice",
    tone: "dark",
    language: "en",
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

/** sanitizeAppearance, güvenilmez bir değerden geçerli bir Appearance üretir. */
export function sanitizeAppearance(raw: unknown, info: PlatformInfo): Appearance {
  const d = defaultAppearance(info);
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return d;
  const o = raw as Record<string, unknown>;
  return {
    version: 1,
    mode: pick(o.mode, modesFor(info), d.mode),
    tint: clampInt(o.tint, LIMITS.tint, d.tint),
    panel: clampInt(o.panel, LIMITS.panel, d.panel),
    edge: clampInt(o.edge, LIMITS.edge, d.edge),
    glow: clampInt(o.glow, LIMITS.glow, d.glow),
    accent: pick(o.accent, ACCENTS, d.accent),
    tone: pick(o.tone, TONES, d.tone),
    language: pick(o.language, LANGUAGE_CODES, d.language),
    uiFont: font(o.uiFont, d.uiFont),
    codeFont: font(o.codeFont, d.codeFont),
  };
}

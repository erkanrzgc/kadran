/**
 * Ayarları CSS değişkenlerine çevirir. DOM'a doğrudan bağlı değil: hedef
 * yalnız style.setProperty taşıyan bir nesne, böylece node --test ile ölçülür.
 *
 * GÜVENLİK: kullanıcının seçtiği yazı tipi ADI buraya hiç girmez. Yığınlar
 * yalnız sabit takma adlardan ("kadran-ui", "kadran-code") ve sabit yedek
 * yığınlardan kurulur; ad yalnız yüzleri seçmek için kullanılır.
 */

import type { Appearance } from "../../shared/appearance.ts";

export const UI_FALLBACK = '"Geist Bundled", "Segoe UI", -apple-system, system-ui, sans-serif';
export const CODE_FALLBACK = '"Geist Mono Bundled", Consolas, ui-monospace, monospace';

export type FontAlias = "kadran-ui" | "kadran-code";

export function fontStack(alias: FontAlias | null, fallback: string): string {
  return alias ? `"${alias}", ${fallback}` : fallback;
}

export interface FontStacks {
  ui: string;
  code: string;
  strong: number;
}

export function appearanceVars(a: Appearance, fonts: FontStacks): Record<string, string> {
  return {
    "--font-ui": fonts.ui,
    "--font-mono": fonts.code,
    "--w-ui": String(a.uiFont.weight),
    "--w-strong": String(fonts.strong),
    "--w-mono": String(a.codeFont.weight),
  };
}

export interface StyleTarget {
  style: { setProperty(name: string, value: string): void };
}

export function applyAppearance(target: StyleTarget, a: Appearance, fonts: FontStacks): void {
  for (const [name, value] of Object.entries(appearanceVars(a, fonts))) target.style.setProperty(name, value);
}

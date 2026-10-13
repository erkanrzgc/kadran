/**
 * Görünüm ayarlarını CSS değişkenlerine ve kök özniteliklerine çevirir.
 * DOM'a doğrudan bağlı değil: hedef yalnız style.setProperty ve dataset
 * taşıyan bir nesne, böylece node --test ile ölçülür.
 *
 * GÜVENLİK: kullanıcının seçtiği yazı tipi ADI buraya hiç girmez. Yığınlar
 * yalnız sabit takma adlardan ("kadran-ui", "kadran-code") ve sabit yedek
 * yığınlardan kurulur; ad yalnız yüzleri seçmek için kullanılır.
 */

import type { Accent, Appearance } from "../../shared/appearance.ts";

export const ACCENT_RGB: Readonly<Record<Accent, string>> = {
  ice: "125, 211, 252",
  amber: "255, 176, 32",
  phosphor: "126, 224, 138",
  paper: "236, 235, 230",
};

export const UI_FALLBACK = '"Segoe UI Variable Text", "Segoe UI", system-ui, sans-serif';
export const CODE_FALLBACK = '"JetBrains Mono Bundled", ui-monospace, Consolas, monospace';

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
    "--tint": String(a.tint / 100),
    "--panel": String(a.panel / 100),
    "--edge": String(a.edge / 100),
    "--glow": String(a.glow / 100),
    "--acc": ACCENT_RGB[a.accent],
    "--font-ui": fonts.ui,
    "--font-mono": fonts.code,
    "--w-ui": String(a.uiFont.weight),
    "--w-strong": String(fonts.strong),
    "--w-mono": String(a.codeFont.weight),
  };
}

export interface StyleTarget {
  style: { setProperty(name: string, value: string): void };
  dataset: Record<string, string | undefined>;
}

export function applyAppearance(target: StyleTarget, a: Appearance, fonts: FontStacks): void {
  for (const [name, value] of Object.entries(appearanceVars(a, fonts))) target.style.setProperty(name, value);
  target.dataset.mode = a.mode;
  target.dataset.tone = a.tone;
}

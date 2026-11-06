/**
 * Tema geçişi (K-163). İki şey birlikte dönmeli:
 *
 * - Pencerenin camı: tonu tema başına, ana süreç kayıtla değiştirir. Kayıt
 *   beklerse arayüz açıkken arkada koyu cam kalırdı (ya da tersi).
 * - Arayüzün renkleri: menü satırı ve düğmeler renk değişimini 120 ms'de
 *   yapıyor, paneller anında; geçişte ekran parça parça değişirdi. Bu yüzden
 *   geçiş anında animasyonlar kapatılır.
 */

import type { Appearance, Theme } from "../../shared/appearance.ts";

/** Yazı tipi ve kalınlık seçimleri her adımda değil, kısa bekleyip yazılır. */
export const SAVE_DELAY_MS = 250;

/** Tema değişince beklemeden kaydedilir: cam ile arayüz aynı anda döner. */
export function saveDelay(prev: Appearance, next: Appearance): number {
  return next.theme !== prev.theme ? 0 : SAVE_DELAY_MS;
}

/** base.css bu sınıf varken bütün animasyonları kapatır. */
export const SWITCHING_CLASS = "theme-switching";

export interface ThemeRoot {
  dataset: Record<string, string | undefined>;
  classList: { add(name: string): void; remove(name: string): void };
}

/**
 * switchTheme temayı animasyonsuz değiştirir: sınıf eklenir, tema değişir,
 * `settle` stilleri o an hesaplatır (renkler animasyonsuz oturur), animasyonlar
 * `later` ile bir sonraki turda geri açılır. İlk çizimde ve aynı temada dokunmaz.
 */
export function switchTheme(root: ThemeRoot, theme: Theme, settle: () => void, later: (fn: () => void) => void): void {
  const current = root.dataset.theme;
  if (current === theme) return;
  if (current === undefined) {
    root.dataset.theme = theme;
    return;
  }
  root.classList.add(SWITCHING_CLASS);
  root.dataset.theme = theme;
  settle();
  later(() => root.classList.remove(SWITCHING_CLASS));
}

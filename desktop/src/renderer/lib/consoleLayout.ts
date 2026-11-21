/** Konsol panelinin yüksekliği: en az 120 px, en çok pencerenin %70'i. */

export const MIN_HEIGHT = 120;
export const MAX_RATIO = 0.7;
/** Go'daki appearance.DefaultConsoleHeight ile aynı. */
export const DEFAULT_HEIGHT = 240;

export function clampHeight(height: number, windowHeight: number): number {
  if (!Number.isFinite(height)) return DEFAULT_HEIGHT;
  const max = Math.max(MIN_HEIGHT, Math.floor(windowHeight * MAX_RATIO));
  return Math.min(max, Math.max(MIN_HEIGHT, Math.round(height)));
}

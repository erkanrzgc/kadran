/**
 * Çizgi grafik örnekleri. Yalnız ekran açıkken toplanır ve bellekte durur;
 * diske ya da sunucuya yazılmaz.
 */

export const MAX_SAMPLES = 60;

export function pushSample(samples: readonly number[], value: number, max = MAX_SAMPLES): number[] {
  if (!Number.isFinite(value)) return [...samples];
  const next = [...samples, value];
  return next.length > max ? next.slice(next.length - max) : next;
}

/** sparklinePath, örnekleri width×height kutusuna SVG yolu olarak çizer. */
export function sparklinePath(samples: readonly number[], width: number, height: number): string {
  if (samples.length < 2) return "";
  const min = Math.min(...samples);
  const max = Math.max(...samples);
  const step = width / (samples.length - 1);
  return samples
    .map((v, i) => {
      const y = max === min ? height / 2 : height - ((v - min) / (max - min)) * height;
      return `${i === 0 ? "M" : "L"}${(i * step).toFixed(1)} ${y.toFixed(1)}`;
    })
    .join(" ");
}

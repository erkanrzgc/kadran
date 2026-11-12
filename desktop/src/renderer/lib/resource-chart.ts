/**
 * Kaynak grafiklerinin geometrisi. Saf: bileşen yalnız çizer.
 * null bir boşluktur (ölçülemeyen aralık): çizgi orada kopar, sıfıra inmez.
 */

/** values'i width×height kutusuna 0..max ölçeğiyle SVG yolu olarak yazar. */
export function chartPath(values: readonly (number | null)[], width: number, height: number, max: number): string {
  if (values.length === 0 || max <= 0) return "";
  const step = values.length > 1 ? width / (values.length - 1) : 0;
  const parts: string[] = [];
  let pen = false;
  values.forEach((v, i) => {
    if (v === null) {
      pen = false;
      return;
    }
    const y = height - (Math.min(v, max) / max) * height;
    parts.push(`${pen ? "L" : "M"}${(i * step).toFixed(1)} ${y.toFixed(1)}`);
    pen = true;
  });
  return parts.join(" ");
}

/** Bayt/sn grafiğinin üst sınırı: en büyük değerin %20 üstü, hiç 0 değil. */
export function scaleMax(values: readonly (number | null)[]): number {
  const top = Math.max(0, ...values.filter((v): v is number => v !== null));
  return top > 0 ? top * 1.2 : 1;
}

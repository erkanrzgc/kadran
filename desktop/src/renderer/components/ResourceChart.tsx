import { useState, type PointerEvent } from "react";

import { chartPath } from "../lib/resource-chart.ts";

const W = 240;
const H = 44;

export interface ChartSeries {
  key: string;
  label: string;
  values: readonly (number | null)[];
  /** Kategorik sıra: 1 mavi, 2 turuncu (tokens.css --series-*). */
  tone: 1 | 2;
}

interface Props {
  series: readonly ChartSeries[];
  /** Ölçeğin üstü (yüzde için 100). */
  max: number;
  format: (value: number) => string;
  label: string;
  testId?: string;
  /**
   * Sabit pencere: bu kadar örnek yeri; az örnek sağa yaslanır (çizgi sağdan
   * dolar, yeni örnekte eskisinin yeri kaymaz). Verilmezse örnek sayısı.
   */
  slots?: number;
}

/**
 * Canlı kaynak grafiği (K-166). Ölçülemeyen aralık boşluk olarak kalır.
 * Üzerine gelince dikey çizgi ve o anın değerleri; iki seride lejant
 * (kimlik yalnız renge kalmasın).
 */
export function ResourceChart({ series: raw, max, format, label, testId, slots = 0 }: Props) {
  const [hover, setHover] = useState<number | null>(null);
  const count = Math.max(slots, ...raw.map((s) => s.values.length));
  const series = raw.map((s) => ({ ...s, values: padStart(s.values, count) }));
  const step = count > 1 ? W / (count - 1) : 0;

  const onMove = (e: PointerEvent<HTMLDivElement>) => {
    if (count === 0) return;
    const box = e.currentTarget.getBoundingClientRect();
    const ratio = box.width > 0 ? (e.clientX - box.left) / box.width : 0;
    setHover(Math.min(count - 1, Math.max(0, Math.round(ratio * (count - 1)))));
  };
  const text = (v: number | null | undefined) => (v === null || v === undefined ? "—" : format(v));
  // Kutu imlecin dışa bakan tarafına açılır: sağ yarıda sola, yoksa karonun dışına taşar.
  const at = hover !== null && count > 1 ? (hover / (count - 1)) * 100 : 0;
  const tipStyle = at > 50 ? { right: `${100 - at}%` } : { left: `${at}%` };

  return (
    <div className="chart" data-testid={testId} onPointerMove={onMove} onPointerLeave={() => setHover(null)}>
      <svg className="chart-svg" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
        <line className="chart-base" x1={0} y1={H} x2={W} y2={H} />
        {series.map((s) => (
          <path key={s.key} className={`chart-line tone-${s.tone}`} d={chartPath(s.values, W, H, max)} />
        ))}
        {hover !== null ? <line className="chart-cross" x1={hover * step} y1={0} x2={hover * step} y2={H} /> : null}
      </svg>
      {hover !== null ? (
        <div className="chart-tip" style={tipStyle} role="status">
          {series.map((s) => (
            <span key={s.key} className="chart-tip-row">
              <i className={`chart-swatch tone-${s.tone}`} aria-hidden="true" />
              {series.length > 1 ? `${s.label} ` : ""}
              <b className="mono">{text(s.values[hover])}</b>
            </span>
          ))}
        </div>
      ) : null}
      {series.length > 1 ? (
        <div className="chart-legend">
          {series.map((s) => (
            <span key={s.key}>
              <i className={`chart-swatch tone-${s.tone}`} aria-hidden="true" />
              {s.label}
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

/** Baştan null ile doldurur: ilk örnekler grafiğin sağında durur. */
function padStart(values: readonly (number | null)[], length: number): readonly (number | null)[] {
  return values.length >= length ? values : [...new Array<null>(length - values.length).fill(null), ...values];
}

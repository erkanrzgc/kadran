import { sparklinePath } from "../lib/samples.ts";

const W = 120;
const H = 28;

/** Küçük çizgi grafik; örnekler yalnız ekran açıkken toplanır (bellekte). */
export function Sparkline({ samples, label }: { samples: readonly number[]; label: string }) {
  const d = sparklinePath(samples, W, H);
  return (
    <svg className="sparkline" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
      {d ? <path d={d} /> : null}
    </svg>
  );
}

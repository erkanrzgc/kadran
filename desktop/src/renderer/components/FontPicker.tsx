interface Props {
  families: readonly string[];
  family: string;
  weight: number;
  weights: readonly number[];
  defaultLabel: string;
  onFamily: (family: string) => void;
  onWeight: (weight: number) => void;
  testId: string;
}

/** Aile + kalınlık seçimi. Boş aile: arayüzde sistem, kodda gömülü JetBrains Mono. */
export function FontPicker({ families, family, weight, weights, defaultLabel, onFamily, onWeight, testId }: Props) {
  return (
    <div className="font-row">
      <select className="sel" value={family} aria-label={defaultLabel} onChange={(e) => onFamily(e.target.value)} data-testid={`${testId}-family`}>
        <option value="">{defaultLabel}</option>
        {families.map((f) => (
          <option key={f} value={f}>{f}</option>
        ))}
      </select>
      <select className="sel sel-w" value={String(weight)} aria-label={`${defaultLabel} · ${weight}`} onChange={(e) => onWeight(Number(e.target.value))} data-testid={`${testId}-weight`}>
        {weights.map((w) => (
          <option key={w} value={String(w)}>{w}</option>
        ))}
      </select>
    </div>
  );
}

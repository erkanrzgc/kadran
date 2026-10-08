interface Props {
  families: readonly string[];
  family: string;
  weight: number;
  weights: readonly number[];
  defaultLabel: string;
  /** Ekran okuyucu adları: aile seçicisi ve kalınlık seçicisi ayrı okunur. */
  label: string;
  weightLabel: string;
  onFamily: (family: string) => void;
  onWeight: (weight: number) => void;
  testId: string;
}

/** Aile + kalınlık seçimi. Boş aile: arayüzde sistem, kodda gömülü JetBrains Mono. */
export function FontPicker({ families, family, weight, weights, defaultLabel, label, weightLabel, onFamily, onWeight, testId }: Props) {
  return (
    <div className="font-row">
      <select className="sel" value={family} aria-label={label} onChange={(e) => onFamily(e.target.value)} data-testid={`${testId}-family`}>
        <option value="">{defaultLabel}</option>
        {families.map((f) => (
          <option key={f} value={f}>{f}</option>
        ))}
      </select>
      <select className="sel sel-w" value={String(weight)} aria-label={weightLabel} onChange={(e) => onWeight(Number(e.target.value))} data-testid={`${testId}-weight`}>
        {weights.map((w) => (
          <option key={w} value={String(w)}>{w}</option>
        ))}
      </select>
    </div>
  );
}

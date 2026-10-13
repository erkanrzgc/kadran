export interface SegOption<T extends string> {
  value: T;
  label: string;
  testId?: string;
}

interface Props<T extends string> {
  options: readonly SegOption<T>[];
  value: T;
  onChange: (value: T) => void;
  label: string;
}

/** Bölümlü seçici; ekran okuyucu için radyo grubu. */
export function Segmented<T extends string>({ options, value, onChange, label }: Props<T>) {
  return (
    <div className="seg" role="radiogroup" aria-label={label}>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          role="radio"
          aria-checked={o.value === value}
          className={o.value === value ? "is-on" : undefined}
          data-testid={o.testId}
          onClick={() => onChange(o.value)}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

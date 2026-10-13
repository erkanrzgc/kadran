import type { ReactNode } from "react";

/** Ayar satırı: solda ad ve tek satırlık açıklama, sağda denetim. */
export function SetRow({ title, hint, children }: { title: string; hint: string; children: ReactNode }) {
  return (
    <div className="set-row">
      <div className="set-label">
        <strong>{title}</strong>
        <span>{hint}</span>
      </div>
      <div className="set-ctl">{children}</div>
    </div>
  );
}

interface RangeRowProps {
  title: string;
  hint: string;
  id: string;
  value: number;
  min: number;
  max: number;
  onChange: (value: number) => void;
}

export function RangeRow({ title, hint, id, value, min, max, onChange }: RangeRowProps) {
  return (
    <SetRow title={title} hint={hint}>
      <input
        type="range"
        id={id}
        min={min}
        max={max}
        value={value}
        aria-label={title}
        data-testid={`range-${id}`}
        onChange={(e) => onChange(Number(e.target.value))}
      />
      <output className="mono" htmlFor={id}>
        {`${value}%`}
      </output>
    </SetRow>
  );
}

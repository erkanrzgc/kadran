import type { ReactNode } from "react";

import type { Tone } from "../lib/chain.ts";

interface Props {
  label: string;
  value: string;
  tone?: Tone;
  testId?: string;
  children?: ReactNode;
}

/** Overview şeridindeki karo: küçük etiket, büyük değer, isteğe bağlı grafik. */
export function Tile({ label, value, tone, testId, children }: Props) {
  return (
    <div className="tile" data-testid={testId}>
      <span className="tile-label">{label}</span>
      <span className={tone ? `tile-value mono is-${tone}` : "tile-value mono"}>{value}</span>
      {children}
    </div>
  );
}

import type { Tone } from "../lib/chain.ts";

export interface KeyValue {
  label: string;
  value: string;
  tone?: Tone;
  mono?: boolean;
}

/** "Sunucu özeti" ızgarası. */
export function KeyValues({ rows, testId }: { rows: readonly KeyValue[]; testId?: string }) {
  return (
    <dl className="kv" data-testid={testId}>
      {rows.map((r) => (
        <div key={r.label}>
          <dt className="kv-label">{r.label}</dt>
          <dd className={["kv-value", r.mono ? "mono" : "", r.tone ? `is-${r.tone}` : ""].filter(Boolean).join(" ")}>{r.value}</dd>
        </div>
      ))}
    </dl>
  );
}

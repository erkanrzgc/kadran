import type { Tone } from "../lib/chain.ts";

export interface InfoRow {
  label: string;
  value: string;
  tone?: Tone;
}

export function InfoCard({ title, rows }: { title: string; rows: readonly InfoRow[] }) {
  return (
    <article className="card glass">
      <h3 className="card-title">{title}</h3>
      <dl className="rows">
        {rows.map((r) => (
          <div className="row" key={r.label}>
            <dt>{r.label}</dt>
            <dd className={r.tone ? `is-${r.tone}` : undefined}>{r.value}</dd>
          </div>
        ))}
      </dl>
    </article>
  );
}

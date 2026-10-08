import { useT } from "../i18n/I18nProvider.tsx";
import { countOutcomes, OUTCOME_KEYS, OUTCOME_TONE } from "../lib/chain.ts";
import type { AuditRecord } from "../lib/types.ts";

/**
 * Yüklü kayıtların sonuç dağılımı (Coolify'daki durum kodu çubuğu gibi).
 * Genişlikler React'in style özelliğiyle (CSSOM) yazılır; CSP style-src
 * yalnız style= NİTELİĞİNİ engeller, bu yol serbest.
 */
export function OutcomeBar({ records }: { records: readonly AuditRecord[] }) {
  const t = useT();
  const counts = countOutcomes(records);
  return (
    <div className="outcomes" data-testid="outcome-bar">
      <div className="outcome-legend">
        {OUTCOME_KEYS.map((k) => (
          <span key={k} className={`legend is-${OUTCOME_TONE[k]}`}>
            <i aria-hidden="true" />
            {t(`outcome.${k}`)}
            <b className="mono">{counts[k]}</b>
          </span>
        ))}
      </div>
      <div className="outcome-track">
        {OUTCOME_KEYS.map((k) =>
          counts[k] > 0 ? <span key={k} className={`outcome-seg is-${OUTCOME_TONE[k]}`} style={{ flexGrow: counts[k] }} /> : null,
        )}
      </div>
    </div>
  );
}

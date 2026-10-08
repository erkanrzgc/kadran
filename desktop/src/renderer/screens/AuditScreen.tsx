import { useEffect, useMemo, useState } from "react";

import { AuditTable } from "../components/AuditTable.tsx";
import { EmptyState } from "../components/EmptyState.tsx";
import { OutcomeBar } from "../components/OutcomeBar.tsx";
import { Segmented } from "../components/Segmented.tsx";
import { useAudit } from "../hooks/useAudit.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { filterRecords, OUTCOME_KEYS, type OutcomeKey } from "../lib/chain.ts";

const RANGES = ["50", "200", "1000"] as const;
type Range = (typeof RANGES)[number];

export function AuditScreen({ target }: { target: string | null }) {
  const t = useT();
  const [range, setRange] = useState<Range>("50");
  const [action, setAction] = useState("");
  const [outcome, setOutcome] = useState<OutcomeKey | "">("");
  const audit = useAudit(target, Number(range));

  // Süzgeç bir sunucuya göre seçilir; hedef değişince sıfırlanır ki
  // öbür sunucunun kayıtları sessizce gizlenmesin. Aralık korunur.
  useEffect(() => {
    setAction("");
    setOutcome("");
  }, [target]);

  const actions = useMemo(() => [...new Set(audit.records.map((r) => r.action))].sort(), [audit.records]);
  const shown = useMemo(() => filterRecords(audit.records, { action, outcome }), [audit.records, action, outcome]);

  if (!target) return <EmptyState title={t("audit.no_target_title")} body={t("audit.no_target_body")} />;

  // Yüklenemeyen liste "kayıt yok" diye gösterilmez (StatusScreen ile aynı).
  const placeholder =
    audit.records.length > 0
      ? shown.length === 0
        ? t("audit.no_match")
        : undefined
      : audit.failed
        ? t("audit.load_failed")
        : audit.loaded
          ? t("audit.empty")
          : t("audit.loading");

  return (
    <div className="screen" data-testid="screen-audit">
      <header className="stage-head">
        <p className="eyebrow">{t("audit.eyebrow")}</p>
        <h1 className="title">{t("audit.title")}</h1>
      </header>

      <section className="panel glass">
        <div className="panel-head">
          <h2 className="section-title">{t("audit.outcomes")}</h2>
          <Segmented
            label={t("audit.range")}
            value={range}
            options={RANGES.map((n) => ({ value: n, label: t("audit.last", { n }), testId: `range-${n}` }))}
            onChange={setRange}
          />
        </div>
        <OutcomeBar records={audit.records} />
      </section>

      <section className="panel glass">
        <div className="panel-head filters">
          <label className="filter">
            {t("audit.filter_action")}
            <select className="sel" value={action} onChange={(e) => setAction(e.target.value)} data-testid="filter-action">
              <option value="">{t("audit.filter_all")}</option>
              {actions.map((a) => (
                <option key={a} value={a}>{a}</option>
              ))}
            </select>
          </label>
          <label className="filter">
            {t("audit.filter_outcome")}
            <select className="sel" value={outcome} onChange={(e) => setOutcome(e.target.value as OutcomeKey | "")} data-testid="filter-outcome">
              <option value="">{t("audit.filter_all")}</option>
              {OUTCOME_KEYS.map((k) => (
                <option key={k} value={k}>{t(`outcome.${k}`)}</option>
              ))}
            </select>
          </label>
          <button type="button" className="btn btn-ghost" onClick={() => void audit.loadNewest()}>
            {t("status.refresh")}
          </button>
        </div>

        <AuditTable records={shown} testId="audit-full" placeholder={placeholder} />

        {audit.hasOlder ? (
          <div className="panel-foot">
            <button type="button" className="btn btn-ghost" disabled={audit.loading} onClick={() => void audit.loadOlder()} data-testid="load-older">
              {audit.loading ? t("audit.loading") : t("audit.load_older")}
            </button>
          </div>
        ) : null}
      </section>
    </div>
  );
}

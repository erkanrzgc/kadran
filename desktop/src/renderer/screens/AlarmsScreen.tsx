import { useCallback } from "react";

import { bridge } from "../bridge.ts";
import { EmptyState } from "../components/EmptyState.tsx";
import { useManagementQuery } from "../hooks/useManagementQuery.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { formatTime } from "../lib/format.ts";

export function AlarmsScreen({ target }: { target: string | null }) {
  const t = useT();
  if (!target) return <EmptyState title={t("alarms.title")} body={t("apps.connect_first")} />;
  return <AlarmList target={target} />;
}

function AlarmList({ target }: { target: string }) {
  const t = useT();
  const language = useLanguage();
  const load = useCallback(() => bridge.alarmsList(target), [target]);
  const query = useManagementQuery(load);
  return <div className="screen" data-testid="screen-alarms"><header className="stage-head resource-head"><div><p className="eyebrow">{t("alarms.eyebrow")}</p><h1 className="title">{t("alarms.title")}</h1><p className="meta">{t("alarms.subtitle")}</p></div><button type="button" className="btn btn-ghost" disabled={query.loading} onClick={() => void query.reload()} data-testid="alarms-refresh">{t("status.refresh")}</button></header>
    {query.data?.delivery_is_local_only ? <p className="scope-banner glass">{t("alarms.local_only")}</p> : null}
    <section className="panel glass">{query.loading ? <p className="meta" role="status">{t("resource.loading")}</p> : null}{query.error ? <p className="operation-message is-bad" role="alert">{query.error}</p> : null}{query.data?.alarms.length === 0 ? <p className="meta" data-testid="alarms-empty">{t("alarms.empty")}</p> : null}
      <div className="alarm-list">{query.data?.alarms.map((alarm) => <article className="alarm-row" key={alarm.id} data-testid="alarm-row"><div className="panel-head"><h2 className="mono">{alarm.target}</h2><span className={`badge is-${alarm.severity === "ALARM_SEVERITY_CRITICAL" ? "bad" : "warn"}`}>{t(`alarm.${alarm.severity}`)}</span></div><p className="meta mono">{alarm.kind}</p><p className="operation-message">{alarm.detail}</p><p className="hint">{t("alarms.since", { time: formatTime(new Date(Number(alarm.since_unix) * 1000).toString(), language) })}</p></article>)}</div>
    </section></div>;
}

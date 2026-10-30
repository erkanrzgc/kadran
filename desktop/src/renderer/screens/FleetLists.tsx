import type { Profile } from "../bridge.ts";
import { OperationsTable } from "../components/OperationsTable.tsx";
import { useRecentRecords } from "../hooks/useRecentRecords.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { mergeOperations, type FleetCard } from "../lib/fleet.ts";
import { formatTime } from "../lib/format.ts";

const AUDIT_ROWS = 100;
const AUDIT_PER_SERVER = 100;

interface ListProps {
  profiles: readonly Profile[];
  cards: readonly FleetCard[];
  onOpen: (target: string, view?: "apps" | "alarms" | "audit") => void;
}

/** Okunamayan sunucu "uygulaması yok" sayılmasın. */
function Unread({ cards }: { cards: readonly FleetCard[] }) {
  const t = useT();
  const names = cards.filter((c) => c.state === "totp" || c.state === "unreachable").map((c) => c.profile.name);
  return names.length > 0 ? <p className="fleet-note" data-testid="fleet-unread">{t("fleet.unread", { names: names.join(", ") })}</p> : null;
}

/** Tüm uygulamalar: salt okunur. Satıra basınca o sunucunun uygulamaları açılır. */
export function FleetApps({ cards, onOpen }: ListProps) {
  const t = useT();
  const rows = cards.flatMap((card) => (card.apps?.apps ?? []).flatMap((app) => (app.spec ? [{ card, app }] : [])));
  return (
    <div className="screen" data-testid="screen-fleet-apps">
      <header className="stage-head"><h1 className="title">{t("nav.apps")}</h1><p className="meta">{t("fleet.apps_subtitle")}</p></header>
      <div className="panel glass">
        <div className="table-wrap">
          <table className="data-table" data-testid="fleet-apps">
            <thead><tr><th scope="col">{t("fleet.col_app")}</th><th scope="col">{t("fleet.col_server")}</th><th scope="col">{t("fleet.col_domain")}</th><th scope="col">{t("fleet.col_branch")}</th><th scope="col">{t("fleet.col_releases")}</th></tr></thead>
            <tbody>
              {rows.length === 0 ? <tr><td colSpan={5} className="table-empty">{t("fleet.no_apps")}</td></tr> : null}
              {rows.map(({ card, app }) => (
                <tr key={`${card.profile.target}:${app.spec.app_id}`} data-testid="fleet-app-row">
                  <td><button type="button" className="row-button mono" onClick={() => onOpen(card.profile.target, "apps")}>{app.spec.app_id}</button></td>
                  <td className="cell-dim cell-fit" title={card.profile.target}>{card.profile.name}</td>
                  <td className="mono cell-dim">{app.spec.domain || "—"}</td>
                  <td className="mono cell-dim">{app.spec.git_branch}</td>
                  <td className="cell-dim">{app.release_count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <Unread cards={cards} />
    </div>
  );
}

/** Tüm alarmlar: salt okunur. */
export function FleetAlarms({ cards, onOpen }: ListProps) {
  const t = useT();
  const language = useLanguage();
  const rows = cards.flatMap((card) => (card.alarms?.alarms ?? []).map((alarm) => ({ card, alarm })));
  return (
    <div className="screen" data-testid="screen-fleet-alarms">
      <header className="stage-head"><h1 className="title">{t("nav.alarms")}</h1><p className="meta">{t("fleet.alarms_subtitle")}</p></header>
      <div className="panel glass">
        <div className="table-wrap">
          <table className="data-table" data-testid="fleet-alarms">
            <thead><tr><th scope="col">{t("fleet.col_server")}</th><th scope="col">{t("fleet.col_alarm")}</th><th scope="col">{t("fleet.col_severity")}</th><th scope="col">{t("fleet.col_detail")}</th><th scope="col">{t("fleet.col_since")}</th></tr></thead>
            <tbody>
              {rows.length === 0 ? <tr><td colSpan={5} className="table-empty" data-testid="fleet-alarms-empty">{t("alarms.empty")}</td></tr> : null}
              {rows.map(({ card, alarm }) => (
                <tr key={`${card.profile.target}:${alarm.id}`} data-testid="fleet-alarm-row">
                  <td><button type="button" className="row-button" onClick={() => onOpen(card.profile.target, "alarms")}>{card.profile.name}</button></td>
                  <td className="mono">{alarm.kind} · {alarm.target}</td>
                  <td><span className={`badge is-${alarm.severity === "ALARM_SEVERITY_CRITICAL" ? "bad" : "warn"}`}>{t(`alarm.${alarm.severity}`)}</span></td>
                  <td className="cell-dim cell-wrap">{alarm.detail}</td>
                  <td className="cell-dim">{formatTime(new Date(Number(alarm.since_unix) * 1000).toISOString(), language)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <Unread cards={cards} />
    </div>
  );
}

/** Bütün sunucuların denetim kayıtları, en yeni üstte. Tam liste sunucunun kendi sayfasında. */
export function FleetAudit({ profiles, onOpen }: ListProps) {
  const t = useT();
  const recent = useRecentRecords(profiles, true, AUDIT_PER_SERVER);
  const failed = recent.failed.map((target) => profiles.find((p) => p.target === target)?.name ?? target);
  const operations = mergeOperations(recent.servers, null, AUDIT_ROWS);
  return (
    <div className="screen" data-testid="screen-fleet-audit">
      <header className="stage-head"><h1 className="title">{t("nav.audit")}</h1><p className="meta">{t("fleet.audit_subtitle")}</p></header>
      <div className="panel glass">
        <OperationsTable operations={operations} profiles={profiles} loaded={recent.loaded} testId="fleet-audit" onOpen={(target) => onOpen(target, "audit")} />
      </div>
      {failed.length > 0 ? <p className="fleet-note" data-testid="ops-failed">{t("fleet.records_failed", { names: failed.join(", ") })}</p> : null}
    </div>
  );
}

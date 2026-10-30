import type { Profile } from "../bridge.ts";
import { OperationsTable } from "../components/OperationsTable.tsx";
import { ServerCards } from "../components/ServerCards.tsx";
import { SetupHelp } from "../components/SetupHelp.tsx";
import { useRecentRecords } from "../hooks/useRecentRecords.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { mergeOperations, OPERATION_ACTIONS, summarize, type FleetCard } from "../lib/fleet.ts";

const RECENT_ROWS = 5;
const PER_SERVER = 100;

export interface FleetActions {
  onOpen: (target: string) => void;
  onNewApp: (target: string) => void;
  onEdit: (target: string) => void;
  onAdd: () => void;
}

interface Props extends FleetActions {
  profiles: readonly Profile[];
  cards: readonly FleetCard[];
  clientVersion: string | null;
  onSeeAll: () => void;
}

function useSummary(cards: readonly FleetCard[]): string {
  const t = useT();
  const s = summarize(cards);
  const parts = [t("fleet.sum_servers", { count: s.servers }), t("fleet.sum_ready", { count: s.ready })];
  if (s.warning > 0) parts.push(t("fleet.sum_warning", { count: s.warning }));
  if (s.signIn > 0) parts.push(t("fleet.sum_sign_in", { count: s.signIn }));
  if (s.failed > 0) parts.push(t("fleet.sum_failed", { count: s.failed }));
  parts.push(t("fleet.app_count", { count: s.apps }));
  return parts.join(" · ");
}

/** "Sunucular" kartı: panoda ve Sunucular ekranında aynı. */
export function ServersCard({ profiles, cards, clientVersion, onOpen, onNewApp, onEdit, onAdd }: Omit<Props, "onSeeAll">) {
  const t = useT();
  return (
    <section className="panel" data-testid="servers-section">
      <div className="panel-head">
        <div>
          <h2 className="section-title">{t("nav.servers")}</h2>
          <p className="panel-sub">{t("fleet.servers_sub")}</p>
        </div>
        <div className="head-actions">
          <button type="button" className="btn btn-ghost" disabled title={t("common.soon")}>{t("fleet.upgrade_all")}</button>
          <button type="button" className="btn btn-primary" onClick={onAdd} data-testid="server-add">{t("fleet.add")}</button>
        </div>
      </div>
      <div className="well">
        {profiles.length === 0 ? <SetupHelp /> : <ServerCards cards={cards} clientVersion={clientVersion} onOpen={onOpen} onNewApp={onNewApp} onEdit={onEdit} />}
      </div>
    </section>
  );
}

export function ServersScreen(props: Omit<Props, "onSeeAll">) {
  const t = useT();
  const summary = useSummary(props.cards);
  return (
    <div className="screen" data-testid="screen-servers">
      <header className="stage-head"><h1 className="title">{t("nav.servers")}</h1><p className="meta" data-testid="fleet-summary">{summary}</p></header>
      <ServersCard {...props} />
    </div>
  );
}

export function Dashboard(props: Props) {
  const t = useT();
  const summary = useSummary(props.cards);
  const recent = useRecentRecords(props.profiles, true, PER_SERVER);
  const operations = mergeOperations(recent.servers, OPERATION_ACTIONS, RECENT_ROWS);
  const failed = recent.failed.map((target) => props.profiles.find((p) => p.target === target)?.name ?? target);
  return (
    <div className="screen" data-testid="screen-dashboard">
      <header className="stage-head"><h1 className="title">{t("nav.dashboard")}</h1><p className="meta" data-testid="fleet-summary">{summary}</p></header>
      <section className="panel">
        <div className="panel-head">
          <div>
            <h2 className="section-title">{t("fleet.recent_title")}</h2>
            <p className="panel-sub">{t("fleet.recent_subtitle")}</p>
          </div>
          <button type="button" className="panel-link" onClick={props.onSeeAll} data-testid="ops-see-all">{t("fleet.see_all")}</button>
        </div>
        <div className="well is-flush">
          <OperationsTable operations={operations} profiles={props.profiles} loaded={recent.loaded} allFailed={recent.servers.length === 0 && failed.length > 0} testId="recent-ops" onOpen={props.onOpen} />
        </div>
      </section>
      {failed.length > 0 ? <p className="fleet-note" style={{ marginTop: -14 }} data-testid="ops-failed">{t("fleet.records_failed", { names: failed.join(", ") })}</p> : null}
      <ServersCard {...props} />
    </div>
  );
}

import type { Profile } from "../bridge.ts";
import { LineIcon } from "../components/LineIcon.tsx";
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

/** Sunucular başlığı ve gruplu kartlar: panoda ve "Sunucular" ekranında aynı. */
export function ServersSection({ profiles, cards, clientVersion, onOpen, onNewApp, onEdit, onAdd }: Omit<Props, "onSeeAll">) {
  const t = useT();
  const s = summarize(cards);
  return (
    <section className="screen" style={{ gap: 12 }} data-testid="servers-section">
      <header className="stage-head resource-head">
        <div>
          <h2 className="title">{t("nav.servers")}</h2>
          <p className="meta" data-testid="fleet-summary">{t("fleet.summary", { servers: s.servers, ready: s.ready, warning: s.warning, unavailable: s.unavailable, apps: s.apps })}</p>
        </div>
        <div className="head-actions">
          <button type="button" className="btn btn-ghost" disabled title={t("common.soon")}><LineIcon name="up" />{t("fleet.upgrade_all")}</button>
          <button type="button" className="btn btn-primary" onClick={onAdd} data-testid="server-add"><LineIcon name="plus" />{t("fleet.add")}</button>
        </div>
      </header>
      {profiles.length === 0 ? <SetupHelp /> : <ServerCards cards={cards} clientVersion={clientVersion} onOpen={onOpen} onNewApp={onNewApp} onEdit={onEdit} />}
    </section>
  );
}

export function Dashboard(props: Props) {
  const t = useT();
  const recent = useRecentRecords(props.profiles, true, PER_SERVER);
  const operations = mergeOperations(recent.servers, OPERATION_ACTIONS, RECENT_ROWS);
  const failed = recent.failed.map((target) => props.profiles.find((p) => p.target === target)?.name ?? target);
  return (
    <div className="screen" data-testid="screen-dashboard">
      <section className="screen" style={{ gap: 12 }}>
        <header className="stage-head">
          <h1 className="title">{t("fleet.recent_title")}</h1>
          <p className="meta">{t("fleet.recent_subtitle")}</p>
        </header>
        <div className="panel glass">
          <div className="panel-head">
            <div>
              <h2 className="section-title">{t("fleet.recent_panel")}</h2>
              <p className="panel-sub">{t("fleet.recent_panel_sub")}</p>
            </div>
            <button type="button" className="btn btn-ghost" onClick={props.onSeeAll} data-testid="ops-see-all">{t("fleet.see_all")}</button>
          </div>
          <OperationsTable operations={operations} profiles={props.profiles} loaded={recent.loaded} testId="recent-ops" onOpen={props.onOpen} />
        </div>
        {failed.length > 0 ? <p className="fleet-note" data-testid="ops-failed">{t("fleet.records_failed", { names: failed.join(", ") })}</p> : null}
      </section>
      <ServersSection {...props} />
    </div>
  );
}

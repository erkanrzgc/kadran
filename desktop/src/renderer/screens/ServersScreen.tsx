import type { Profile } from "../bridge.ts";
import { ServerCards } from "../components/ServerCards.tsx";
import { SetupHelp } from "../components/SetupHelp.tsx";
import { useFleetResources } from "../hooks/useFleetResources.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { summarize, type FleetCard } from "../lib/fleet.ts";

interface Props {
  profiles: readonly Profile[];
  cards: readonly FleetCard[];
  clientVersion: string | null;
  onOpen: (target: string) => void;
  onNewApp: (target: string) => void;
  onEdit: (target: string) => void;
  onAdd: () => void;
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

/** Kart ölçerlerinin yoklama aralığı (K-166). */
const METER_POLL_MS = 5_000;

/** Uygulamanın açılış ekranı: bütün sunucular, sağlayıcıya göre gruplu. */
export function ServersScreen({ profiles, cards, clientVersion, onOpen, onNewApp, onEdit, onAdd }: Props) {
  const t = useT();
  const summary = useSummary(cards);
  // Yalnız bağlı sunucular: erişilemeyen ya da giriş bekleyene her turda bağlanılmaz.
  const meters = useFleetResources(cards.filter((c) => c.state === "ready").map((c) => c.profile.target), METER_POLL_MS);
  return (
    <div className="screen" data-testid="screen-servers">
      <header className="stage-head"><h1 className="title">{t("nav.servers")}</h1><p className="meta" data-testid="fleet-summary">{summary}</p></header>
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
          {profiles.length === 0 ? <SetupHelp /> : <ServerCards cards={cards} clientVersion={clientVersion} meters={meters} onOpen={onOpen} onNewApp={onNewApp} onEdit={onEdit} />}
        </div>
      </section>
    </div>
  );
}

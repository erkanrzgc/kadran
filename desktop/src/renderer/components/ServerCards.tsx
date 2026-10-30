import { useT } from "../i18n/I18nProvider.tsx";
import { diskUsedPercent, groupByProvider, updateAvailable, type FleetCard } from "../lib/fleet.ts";
import { versionLabel } from "../lib/format.ts";
import { providerView } from "../lib/providers.ts";
import { LineIcon } from "./LineIcon.tsx";

interface Props {
  cards: readonly FleetCard[];
  clientVersion: string | null;
  onOpen: (target: string) => void;
  onNewApp: (target: string) => void;
  onEdit: (target: string) => void;
}

/** Sağlayıcıya göre gruplu sunucu kartları. Birden çok kartlı grup tam satır kaplar. */
export function ServerCards({ cards, clientVersion, onOpen, onNewApp, onEdit }: Props) {
  const t = useT();
  return (
    <div className="groups" data-testid="server-groups">
      {groupByProvider(cards).map((group) => {
        const provider = providerView(group.provider);
        return (
          <section className={group.cards.length > 1 ? "group is-wide" : "group"} key={group.provider} data-testid={`group-${group.provider || "none"}`}>
            <h3 className="grup-bas">
              <span className="rozet" style={provider.color ? { background: provider.color } : undefined}>{provider.letter}</span>
              {/* Marka adı Türkçe büyük harfe çevrilmesin (HOSTİNGER). */}
              {provider.name ? <span lang="en">{provider.name}</span> : t("fleet.no_provider")}
            </h3>
            <div className="izgara">
              {group.cards.map((card) => <Card key={card.profile.target} card={card} clientVersion={clientVersion} onOpen={onOpen} onNewApp={onNewApp} onEdit={onEdit} />)}
            </div>
          </section>
        );
      })}
    </div>
  );
}

function subtitle(card: FleetCard, t: ReturnType<typeof useT>): string {
  if (card.state === "loading") return t("fleet.loading");
  if (card.state === "totp") return t("fleet.totp_required");
  if (card.state === "unreachable") return t("fleet.unreachable");
  const parts = [card.status?.host?.os];
  const disk = diskUsedPercent(card.status?.host ?? null);
  if (disk !== null) parts.push(t("fleet.disk", { percent: disk }));
  if (card.alarmCount === null) parts.push(t("fleet.alarms_unknown"));
  else if (card.alarmCount > 0) parts.push(t("fleet.alarm_count", { count: card.alarmCount }));
  return parts.filter(Boolean).join(" · ");
}

function Card({ card, clientVersion, onOpen, onNewApp, onEdit }: { card: FleetCard; clientVersion: string | null; onOpen: (target: string) => void; onNewApp: (target: string) => void; onEdit: (target: string) => void }) {
  const t = useT();
  const { target, name } = card.profile;
  const version = card.status?.daemon_version ?? "";
  const stale = clientVersion !== null && updateAvailable(version, clientVersion);
  return (
    <article className="kart glass" data-testid={`card-${target}`} data-state={card.state}>
      <button type="button" className="kart-main" title={card.failure ?? target} onClick={() => onOpen(target)} data-testid={`server-${target}`}>
        <span className="ikon"><LineIcon name="server" /></span>
        <span className="kart-text">
          <span className="kart-ad"><span>{name}</span><span className={`dot is-${card.tone}`} aria-hidden="true" /></span>
          <span className="kart-acik">{subtitle(card, t)}</span>
        </span>
      </button>
      <div className="kart-alt">
        {card.state === "totp" ? (
          <span className="mono">{t("fleet.totp_hint")}</span>
        ) : stale ? (
          <span className="mono is-warn">{versionLabel(version)} → {versionLabel(clientVersion)}</span>
        ) : (
          <span>
            {card.appCount !== null ? t("fleet.app_count", { count: card.appCount }) : "—"}
            {version ? <> · <span className="mono">{versionLabel(version)}</span></> : null}
          </span>
        )}
        <span className="eylem">
          <button type="button" className="icon-btn" title={t("fleet.new_app")} aria-label={t("fleet.new_app")} disabled={card.state !== "ready"} onClick={() => onNewApp(target)} data-testid={`card-new-${target}`}><LineIcon name="plus" size={12} /></button>
          <button type="button" className="icon-btn" title={t("fleet.edit")} aria-label={t("fleet.edit")} onClick={() => onEdit(target)} data-testid={`card-edit-${target}`}><LineIcon name="settings" size={12} /></button>
        </span>
      </div>
    </article>
  );
}

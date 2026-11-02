import { useT } from "../i18n/I18nProvider.tsx";
import { diskUsedPercent, isBrokenInstall, orderByProvider, updateAvailable, type FleetCard } from "../lib/fleet.ts";
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

/** Sunucu kartları: aynı sağlayıcı yan yana, her kartın üstünde sağlayıcısı. */
export function ServerCards({ cards, clientVersion, onOpen, onNewApp, onEdit }: Props) {
  const t = useT();
  return (
    <div className="izgara" data-testid="server-groups">
      {orderByProvider(cards).map((card) => {
        const provider = providerView(card.profile.provider ?? "");
        return (
          <div key={card.profile.target} data-provider={card.profile.provider ?? ""}>
            <div className="sag-ad">
              <span className="rozet" style={provider.color ? { background: provider.color } : undefined}>{provider.letter}</span>
              {/* Marka adı Türkçe büyük harfe çevrilmesin (HOSTİNGER). */}
              <span lang="en">{provider.name || t("fleet.no_provider")}</span>
            </div>
            <Card card={card} clientVersion={clientVersion} onOpen={onOpen} onNewApp={onNewApp} onEdit={onEdit} />
          </div>
        );
      })}
    </div>
  );
}

function subtitle(card: FleetCard, t: ReturnType<typeof useT>): string {
  if (card.state === "loading") return t("fleet.loading");
  if (card.state === "totp") return t("fleet.totp_required");
  if (card.state === "unreachable") return t("fleet.unreachable");
  if (isBrokenInstall(card.status)) return t("value.broken_install", { user: "root" });
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
    <article className="kart" data-testid={`card-${target}`} data-state={card.state} data-tone={card.tone}>
      <button type="button" className="kart-main" title={card.failure ?? target} onClick={() => onOpen(target)} data-testid={`server-${target}`}>
        <span className="kart-ad"><span>{name}</span><span className={`dot is-${card.tone}`} aria-hidden="true" /></span>
        <span className="kart-acik">{subtitle(card, t)}</span>
      </button>
      <span className="eylem">
        <button type="button" className="icon-btn" title={t("fleet.new_app")} aria-label={t("fleet.new_app")} disabled={card.state !== "ready"} onClick={() => onNewApp(target)} data-testid={`card-new-${target}`}><LineIcon name="plus" size={13} /></button>
        <button type="button" className="icon-btn" title={t("fleet.edit")} aria-label={t("fleet.edit")} onClick={() => onEdit(target)} data-testid={`card-edit-${target}`}><LineIcon name="settings" size={13} /></button>
      </span>
      <div className="kart-alt">
        {card.state === "totp" ? (
          <span className="mono">{t("fleet.totp_hint")}</span>
        ) : (
          <>
            <span>{card.appCount !== null ? t("fleet.app_count", { count: card.appCount }) : "—"}</span>
            {version ? <span className={stale ? "mono is-warn" : "mono"}>{stale && clientVersion ? `${versionLabel(version)} → ${versionLabel(clientVersion)}` : versionLabel(version)}</span> : null}
          </>
        )}
      </div>
    </article>
  );
}

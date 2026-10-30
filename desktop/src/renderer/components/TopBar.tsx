import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { healthKey, healthTone, type Health } from "../lib/health.ts";
import { CrumbMenu } from "./CrumbMenu.tsx";
import type { FleetView } from "./Rail.tsx";

const FLEET_VIEWS: readonly FleetView[] = ["dashboard", "servers", "apps", "alarms", "audit", "settings"];

interface CommonProps {
  profiles: readonly Profile[];
  onOpenServer: (target: string) => void;
  onNavigate: (view: FleetView) => void;
}

interface FleetProps extends CommonProps {
  mode: "fleet";
  view: FleetView;
}

interface ServerProps extends CommonProps {
  mode: "server";
  target: string;
  health: Health;
  rtt: number | null;
  verifying: boolean;
  onVerify: () => void;
}

const nameOf = (profiles: readonly Profile[], target: string): string => profiles.find((p) => p.target === target)?.name ?? target;

/** Yol çubuğu. Filoda `Filo ⌄ / Pano ⌄`, sunucuda `Sunucular / ad ⌄ [sağlık]` + birincil eylem. */
export function TopBar(props: FleetProps | ServerProps) {
  const t = useT();
  const servers = props.profiles.map((p) => ({ key: p.target, label: p.name, current: props.mode === "server" && p.target === props.target, onSelect: () => props.onOpenServer(p.target) }));
  if (props.mode === "fleet") {
    return (
      <header className="topbar">
        <nav className="crumbs" aria-label={t("topbar.crumbs")}>
          <CrumbMenu label={t("topbar.fleet")} items={servers} empty={t("topbar.no_servers")} testId="switcher" />
          <span className="crumb-sep" aria-hidden="true">/</span>
          <CrumbMenu label={t(`nav.${props.view}`)} items={FLEET_VIEWS.map((v) => ({ key: v, label: t(`nav.${v}`), current: v === props.view, onSelect: () => props.onNavigate(v) }))} testId="view-menu" />
        </nav>
      </header>
    );
  }
  return (
    <header className="topbar">
      <nav className="crumbs" aria-label={t("topbar.crumbs")}>
        <button type="button" className="crumb" onClick={() => props.onNavigate("servers")} data-testid="crumb-servers"><span>{t("nav.servers")}</span></button>
        <span className="crumb-sep" aria-hidden="true">/</span>
        <CrumbMenu label={nameOf(props.profiles, props.target)} items={servers} testId="switcher" />
        <span className={`pill is-${healthTone(props.health)}`} data-testid="health">
          <i className="pill-dot" aria-hidden="true" />
          {t(healthKey(props.health))}
        </span>
      </nav>
      <div className="topbar-right">
        {props.rtt !== null ? (
          <span className="pill is-muted mono" title={t("topbar.rtt_title")} data-testid="rtt">
            {t("topbar.rtt", { ms: props.rtt })}
          </span>
        ) : null}
        <button type="button" className="btn btn-primary" disabled={props.verifying} onClick={props.onVerify} data-testid="verify">
          {props.verifying ? t("topbar.verifying") : t("topbar.verify")}
        </button>
      </div>
    </header>
  );
}

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { healthKey, healthTone, type Health } from "../lib/health.ts";
import { ServerSwitcher } from "./ServerSwitcher.tsx";

interface Props {
  target: string | null;
  profiles: readonly Profile[];
  health: Health;
  rtt: number | null;
  verifying: boolean;
  onVerify: () => void;
  onSwitch: (target: string) => void;
}

/** Üst çubuk (Coolify düzeni): kırıntı yolu + sağlık hapı | gecikme + birincil eylem. */
export function TopBar({ target, profiles, health, rtt, verifying, onVerify, onSwitch }: Props) {
  const t = useT();
  return (
    <header className="topbar">
      <nav className="crumbs" aria-label={t("topbar.crumbs")}>
        <span className="crumb-static">{t("topbar.servers")}</span>
        <span className="crumb-sep" aria-hidden="true">/</span>
        <ServerSwitcher target={target} profiles={profiles} onSwitch={onSwitch} />
        {target ? (
          <span className={`pill is-${healthTone(health)}`} data-testid="health">
            <i className="pill-dot" aria-hidden="true" />
            {t(healthKey(health))}
          </span>
        ) : null}
      </nav>
      <div className="topbar-right">
        {rtt !== null ? (
          <span className="pill is-muted mono" title={t("topbar.rtt_title")} data-testid="rtt">
            {t("topbar.rtt", { ms: rtt })}
          </span>
        ) : null}
        <button type="button" className="btn btn-primary" disabled={!target || verifying} onClick={onVerify} data-testid="verify">
          {verifying ? t("topbar.verifying") : t("topbar.verify")}
        </button>
      </div>
    </header>
  );
}

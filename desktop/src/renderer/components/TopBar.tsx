import type { ReactNode } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { healthKey, healthTone, type Health } from "../lib/health.ts";
import { CrumbMenu } from "./CrumbMenu.tsx";
import type { FleetView } from "./Rail.tsx";
import { WindowButtons } from "./WindowButtons.tsx";

interface Props {
  /** Açık sunucu; yoksa filo ekranı. */
  target: string | null;
  health: Health;
  profiles: readonly Profile[];
  /** Sekmeler. */
  children: ReactNode;
  onOpenServer: (target: string) => void;
  onNavigate: (view: FleetView) => void;
}

/**
 * Düz üst çubuk (48 px): yol, sekmeler, pencere düğmeleri. Filo ekranında yol
 * yok: sayfa adı zaten sayfanın başlığında ve kenar çubuğunda seçili.
 */
export function TopBar({ target, health, profiles, children, onOpenServer, onNavigate }: Props) {
  const t = useT();
  const servers = profiles.map((p) => ({ key: p.target, label: p.name, current: p.target === target, onSelect: () => onOpenServer(p.target) }));
  const name = profiles.find((p) => p.target === target)?.name ?? target ?? "";
  return (
    <header className="topbar">
      {target === null ? null : (
        <nav className="crumbs" aria-label={t("topbar.crumbs")}>
          <button type="button" className="crumb is-quiet" onClick={() => onNavigate("servers")} data-testid="crumb-servers"><span>{t("nav.servers")}</span></button>
          <span className="crumb-sep" aria-hidden="true">/</span>
          <CrumbMenu label={name} items={servers} testId="switcher" />
          <span className={`pill is-${healthTone(health)}`} data-testid="health">
            <i className="pill-dot" aria-hidden="true" />
            <span style={{ color: "var(--fg)" }}>{t(healthKey(health))}</span>
          </span>
        </nav>
      )}
      {children}
      <WindowButtons />
    </header>
  );
}

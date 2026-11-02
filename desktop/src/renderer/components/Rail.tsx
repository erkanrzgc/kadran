import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { railVersion, versionLabel } from "../lib/format.ts";
import type { VersionPayload } from "../lib/types.ts";
import { BrandWordmark } from "./BrandWordmark.tsx";
import { LineIcon } from "./LineIcon.tsx";
import { NavButton } from "./NavButton.tsx";

export type FleetView = "servers" | "apps" | "alarms" | "audit" | "settings";

interface Props {
  view: FleetView | null;
  serverOpen: boolean;
  alarmCount: number;
  version: VersionPayload | null;
  onNavigate: (view: FleetView) => void;
}

const isMac = bridge.platform === "darwin";

export function Rail({ view, serverOpen, alarmCount, version, onNavigate }: Props) {
  const t = useT();
  const soon = t("common.soon");
  const item = (name: FleetView) => ({ active: !serverOpen && view === name, onClick: () => onNavigate(name), testId: name === "settings" ? "nav-settings" : `rail-${name}` });

  return (
    <aside className={isMac ? "rail is-mac" : "rail"}>
      <div className="rail-brand">
        <BrandWordmark label={t("brand.logo")} />
      </div>
      <button type="button" className="search" disabled title={soon}>
        <LineIcon name="search" size={14} />
        <span>{t("nav.search")}</span>
      </button>

      <div className="nav-group">
        <p className="nav-section">{t("nav.section_fleet")}</p>
        <nav className="nav" aria-label={t("nav.label")}>
          <NavButton icon="server" label={t("nav.servers")} {...item("servers")} active={serverOpen || view === "servers"} />
          <NavButton icon="layers" label={t("nav.apps")} {...item("apps")} />
          <NavButton icon="bell" label={t("nav.alarms")} {...item("alarms")} count={alarmCount} />
        </nav>
      </div>
      <div className="nav-group">
        <p className="nav-section">{t("nav.section_security")}</p>
        <nav className="nav" aria-label={t("nav.section_security")}>
          <NavButton icon="key" label={t("nav.keys")} soon={soon} />
          <NavButton icon="shield" label={t("nav.audit")} {...item("audit")} />
        </nav>
      </div>
      <div className="nav-group">
        <p className="nav-section">{t("nav.section_manage")}</p>
        <nav className="nav" aria-label={t("nav.section_manage")}>
          <NavButton icon="mail" label={t("nav.notifications")} soon={soon} />
          <NavButton icon="settings" label={t("nav.settings")} {...item("settings")} />
        </nav>
      </div>

      <div className="rail-foot">
        <span>Kadran</span>
        <span className="rail-version" data-testid="rail-version" title={version ? versionLabel(version.version) : undefined}>{version ? railVersion(version.version) : "…"}</span>
      </div>
    </aside>
  );
}

import { useEffect, useState } from "react";

import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { versionLabel } from "../lib/format.ts";
import type { VersionPayload } from "../lib/types.ts";
import { BrandWordmark } from "./BrandWordmark.tsx";
import { LineIcon } from "./LineIcon.tsx";
import { NavButton } from "./NavButton.tsx";

export type FleetView = "dashboard" | "servers" | "apps" | "alarms" | "audit" | "settings";

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
  const [collapsed, setCollapsed] = useState(false);
  const [user, setUser] = useState("");

  useEffect(() => {
    // Ad yalnız görünüş; okunamazsa kenar çubuğunun altı boş kalır.
    bridge.userName().then(setUser, () => undefined);
  }, []);

  useEffect(() => {
    const toggle = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "b") {
        event.preventDefault();
        setCollapsed((c) => !c);
      }
    };
    document.addEventListener("keydown", toggle);
    return () => document.removeEventListener("keydown", toggle);
  }, []);

  const soon = t("common.soon");
  const item = (name: FleetView) => ({ active: !serverOpen && view === name, onClick: () => onNavigate(name), testId: name === "settings" ? "nav-settings" : `rail-${name}` });

  return (
    <aside className={["rail", collapsed ? "is-collapsed" : "", isMac ? "is-mac" : ""].filter(Boolean).join(" ")}>
      <div className="rail-brand">
        <BrandWordmark label={t("brand.logo")} />
        <span className="rail-version" data-testid="rail-version">{version ? versionLabel(version.version) : "…"}</span>
      </div>
      <button type="button" className="search" disabled title={soon}>
        <LineIcon name="search" size={14} />
        <span>{t("nav.search")}</span>
        <span className="kbd">{isMac ? "⌘K" : "Ctrl K"}</span>
      </button>

      <div className="nav-group">
        <p className="nav-section">{t("nav.section_fleet")}</p>
        <nav className="nav" aria-label={t("nav.label")}>
          <NavButton icon="dashboard" label={t("nav.dashboard")} {...item("dashboard")} />
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
        <div className="user" data-testid="rail-user">
          <b>{user ? user[0]!.toUpperCase() : "·"}</b>
          <span>{user}</span>
        </div>
        <button type="button" className="rail-toggle" title={t("nav.toggle")} aria-label={t("nav.toggle")} onClick={() => setCollapsed((c) => !c)} data-testid="rail-toggle">
          <span className="kbd">{isMac ? "⌘B" : "Ctrl B"}</span>
        </button>
      </div>
    </aside>
  );
}

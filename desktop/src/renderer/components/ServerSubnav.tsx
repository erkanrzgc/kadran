import { useT } from "../i18n/I18nProvider.tsx";
import type { LineIconName } from "./LineIcon.tsx";
import { NavButton } from "./NavButton.tsx";
import { NavSlider } from "./NavSlider.tsx";

export type ServerView = "status" | "apps" | "alarms" | "backups" | "audit";

/** Sunucu sayfasının ikinci menüsü; dar pencerede CSS onu tek satıra çevirir. */
export function ServerSubnav({ view, onNavigate }: { view: ServerView; onNavigate: (view: ServerView) => void }) {
  const t = useT();
  const soon = t("common.soon");
  const button = (name: ServerView, icon: LineIconName, key: string) => (
    <NavButton icon={icon} label={t(key)} active={view === name} onClick={() => onNavigate(name)} testId={`nav-${name}`} />
  );
  return (
    <aside className="subnav" aria-label={t("sub.label")}>
      <NavSlider activeKey={view} />
      <div className="nav-group">
        <p className="nav-section">{t("sub.section_server")}</p>
        <nav className="nav">
          {button("status", "server", "sub.status")}
          {button("apps", "layers", "sub.apps")}
          {button("alarms", "bell", "sub.alarms")}
          {button("backups", "database", "sub.backups")}
        </nav>
      </div>
      <div className="nav-group">
        <p className="nav-section">{t("nav.section_security")}</p>
        <nav className="nav">
          <NavButton icon="key" label={t("sub.totp")} soon={soon} />
          {button("audit", "list", "sub.audit")}
        </nav>
      </div>
      <div className="nav-group">
        <p className="nav-section">{t("sub.section_maintenance")}</p>
        <nav className="nav">
          <NavButton icon="up" label={t("sub.upgrade")} soon={soon} />
          <NavButton icon="scissors" label={t("sub.prune")} soon={soon} />
        </nav>
      </div>
    </aside>
  );
}

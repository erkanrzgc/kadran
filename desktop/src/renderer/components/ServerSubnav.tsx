import { useT } from "../i18n/I18nProvider.tsx";
import { LineIcon, type LineIconName } from "./LineIcon.tsx";
import { NavButton } from "./NavButton.tsx";

export type ServerView = "status" | "apps" | "alarms" | "backups" | "audit";

const ITEMS: readonly { view: ServerView; icon: LineIconName; key: string }[] = [
  { view: "status", icon: "server", key: "sub.status" },
  { view: "apps", icon: "layers", key: "sub.apps" },
  { view: "alarms", icon: "bell", key: "sub.alarms" },
  { view: "backups", icon: "database", key: "sub.backups" },
  { view: "audit", icon: "link", key: "sub.audit" },
];

/** Sunucu sayfasının iki katlı menüsü; dar pencerede yatay şerit olur. */
export function ServerSubnav({ view, onNavigate }: { view: ServerView; onNavigate: (view: ServerView) => void }) {
  const t = useT();
  const soon = t("common.soon");
  const button = (view_: ServerView, icon: LineIconName, key: string) => (
    <NavButton icon={icon} label={t(key)} active={view === view_} onClick={() => onNavigate(view_)} testId={`nav-${view_}`} />
  );
  return (
    <aside className="subnav" aria-label={t("sub.label")}>
      <p className="nav-section">{t("sub.section_server")}</p>
      <nav className="nav">
        {button("status", "server", "sub.status")}
        {button("apps", "layers", "sub.apps")}
        {button("alarms", "bell", "sub.alarms")}
        {button("backups", "database", "sub.backups")}
      </nav>
      <p className="nav-section">{t("nav.section_security")}</p>
      <nav className="nav">
        <NavButton icon="shield" label={t("sub.totp")} soon={soon} />
        {button("audit", "link", "sub.audit")}
      </nav>
      <p className="nav-section">{t("sub.section_maintenance")}</p>
      <nav className="nav">
        <NavButton icon="up" label={t("sub.upgrade")} soon={soon} />
        <NavButton icon="scissors" label={t("sub.prune")} soon={soon} />
      </nav>
    </aside>
  );
}

/** Dar pencere: aynı hedefler tek satırda. Test kimlikleri geniş menüde kalır. */
export function ServerSubnavRow({ view, onNavigate }: { view: ServerView; onNavigate: (view: ServerView) => void }) {
  const t = useT();
  return (
    <nav className="subnav-row" aria-label={t("sub.label")}>
      {ITEMS.map((item) => (
        <button type="button" key={item.view} className={view === item.view ? "nav-item is-active" : "nav-item"} aria-current={view === item.view ? "page" : undefined} onClick={() => onNavigate(item.view)}>
          <LineIcon name={item.icon} />
          <span className="nav-label">{t(item.key)}</span>
        </button>
      ))}
    </nav>
  );
}

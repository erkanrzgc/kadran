import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import type { FleetCard } from "../lib/fleet.ts";
import { LineIcon } from "./LineIcon.tsx";

interface Props {
  tabs: readonly string[];
  active: string | null;
  profiles: readonly Profile[];
  cards: readonly FleetCard[];
  onSelectFleet: () => void;
  onSelect: (target: string) => void;
  onClose: (target: string) => void;
}

/** Tarayıcı gibi sekmeler: "Pano" ve açılmış sunucular. */
export function Tabs({ tabs, active, profiles, cards, onSelectFleet, onSelect, onClose }: Props) {
  const t = useT();
  return (
    <nav className="tabs" aria-label={t("tabs.label")}>
      <div className={active === null ? "tab is-on" : "tab"}>
        <button type="button" className="tab-main" onClick={onSelectFleet} data-testid="tab-fleet"><LineIcon name="dashboard" /><span>{t("nav.dashboard")}</span></button>
      </div>
      {tabs.map((target) => {
        const name = profiles.find((p) => p.target === target)?.name ?? target;
        const tone = cards.find((c) => c.profile.target === target)?.tone ?? "muted";
        return (
          <div className={active === target ? "tab is-on" : "tab"} key={target} data-testid={`tab-${target}`}>
            <button type="button" className="tab-main" title={target} onClick={() => onSelect(target)}><span className={`dot is-${tone}`} aria-hidden="true" /><span>{name}</span></button>
            <button type="button" className="tab-close" aria-label={t("tabs.close", { name })} title={t("tabs.close", { name })} onClick={() => onClose(target)} data-testid={`tab-close-${target}`}><LineIcon name="close" size={12} /></button>
          </div>
        );
      })}
    </nav>
  );
}

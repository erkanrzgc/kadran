import { useEffect, useRef, type WheelEvent } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import type { FleetCard } from "../lib/fleet.ts";
import { LineIcon } from "./LineIcon.tsx";

interface Props {
  tabs: readonly string[];
  active: string | null;
  profiles: readonly Profile[];
  cards: readonly FleetCard[];
  onSelect: (target: string) => void;
  onClose: (target: string) => void;
}

/** Tek sunucuda üst çubuktaki yol yeter; şerit ancak iki sunucu açıkken çıkar. */
const MIN_TABS = 2;

/** Açık sunucular arasında geçiş için sekmeler. */
export function Tabs({ tabs, active, profiles, cards, onSelect, onClose }: Props) {
  const t = useT();
  const strip = useRef<HTMLElement>(null);

  // Açık sekme her zaman görünür kalsın; kaydırma çubuğu gizli.
  useEffect(() => {
    strip.current?.querySelector(".tab.is-on")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [active, tabs]);

  const wheel = (event: WheelEvent<HTMLElement>) => {
    event.currentTarget.scrollLeft += event.deltaY;
  };

  if (tabs.length < MIN_TABS) return null;

  return (
    <nav className="tabs" aria-label={t("tabs.label")} ref={strip} onWheel={wheel}>
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

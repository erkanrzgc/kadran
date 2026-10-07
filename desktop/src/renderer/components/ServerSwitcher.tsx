import { useEffect, useRef, useState } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { ChevronDownIcon } from "../icons/index.ts";
import type { AnimatedIconHandle } from "./NavButton.tsx";

interface Props {
  target: string | null;
  profiles: readonly Profile[];
  onSwitch: (target: string) => void;
}

/** Kırıntı yolundaki sunucu seçici (Coolify düzeni): `Servers / hetzner ▾`. */
export function ServerSwitcher({ target, profiles, onSwitch }: Props) {
  const t = useT();
  const [open, setOpen] = useState(false);
  const icon = useRef<AnimatedIconHandle>(null);
  const box = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("keydown", esc);
    };
  }, [open]);

  return (
    <div className="switcher" ref={box}>
      <button
        type="button"
        className="crumb crumb-current mono"
        aria-haspopup="listbox"
        aria-expanded={open}
        data-testid="switcher"
        onClick={() => setOpen((o) => !o)}
        onMouseEnter={() => icon.current?.startAnimation()}
        onMouseLeave={() => icon.current?.stopAnimation()}
      >
        {target ?? t("topbar.not_connected")}
        <ChevronDownIcon ref={icon} size={14} />
      </button>
      {open ? (
        <ul className="switcher-menu glass" role="listbox">
          {profiles.length === 0 ? (
            <li className="empty">{t("rail.no_saved")}</li>
          ) : (
            profiles.map((p) => (
              <li key={p.target}>
                <button
                  type="button"
                  role="option"
                  aria-selected={p.target === target}
                  className="server"
                  onClick={() => {
                    setOpen(false);
                    onSwitch(p.target);
                  }}
                >
                  {p.name}
                </button>
              </li>
            ))
          )}
        </ul>
      ) : null}
    </div>
  );
}

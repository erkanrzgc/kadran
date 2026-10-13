import { useState, type FormEvent } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { ActivityIcon, BadgeAlertIcon, HistoryIcon, ServerIcon, SettingsIcon, ShieldCheckIcon } from "../icons/index.ts";
import type { Tone } from "../lib/chain.ts";
import { versionLabel } from "../lib/format.ts";
import type { VersionPayload } from "../lib/types.ts";
import { NavButton } from "./NavButton.tsx";

export type View = "status" | "apps" | "alarms" | "backups" | "audit" | "settings";

interface Props {
  profiles: readonly Profile[];
  currentTarget: string | null;
  currentTone: Tone;
  view: View;
  connecting: boolean;
  version: VersionPayload | null;
  onConnect: (target: string) => void;
  onNavigate: (view: View) => void;
}

export function Rail({ profiles, currentTarget, currentTone, view, connecting, version, onConnect, onNavigate }: Props) {
  const t = useT();
  const [target, setTarget] = useState("");

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const value = target.trim();
    if (value) onConnect(value);
  };

  return (
    <aside className="rail glass">
      <form className="connect" onSubmit={submit}>
        <label className="label" htmlFor="target">{t("rail.target_label")}</label>
        <div className="input-row">
          <input
            id="target"
            className="field mono"
            value={target}
            onChange={(e) => setTarget(e.target.value)}
            placeholder={t("rail.target_placeholder")}
            spellCheck={false}
            autoComplete="off"
            data-testid="target-input"
          />
          <button className="btn btn-primary" type="submit" disabled={connecting || target.trim() === ""} data-testid="connect">
            {connecting ? t("rail.connecting") : t("rail.connect")}
          </button>
        </div>
        <p className="hint">{t("rail.hint")}</p>
        <details className="connection-help"><summary>{t("setup.connection_help")}</summary><p>{t("setup.compact_help")}</p></details>
      </form>

      <p className="label">{t("rail.saved")}</p>
      {profiles.length === 0 ? (
        <p className="empty">{t("rail.no_saved")}</p>
      ) : (
        <ul className="servers">
          {profiles.map((p) => {
            const active = p.target === currentTarget;
            return (
              <li key={p.target}>
                <button
                  type="button"
                  className={active ? "server is-active" : "server"}
                  aria-current={active ? "true" : undefined}
                  title={p.target}
                  data-testid={`server-${p.target}`}
                  onClick={() => {
                    setTarget(p.target);
                    onConnect(p.target);
                  }}
                >
                  <span className={active ? `dot is-${currentTone}` : "dot"} aria-hidden="true" />
                  <span className="server-name">{p.name}</span>
                </button>
              </li>
            );
          })}
        </ul>
      )}

      <nav className="nav" aria-label={t("nav.label")}>
        <NavButton icon={ActivityIcon} label={t("nav.status")} active={view === "status"} onClick={() => onNavigate("status")} testId="nav-status" />
        <NavButton icon={ServerIcon} label={t("nav.apps")} active={view === "apps"} onClick={() => onNavigate("apps")} testId="nav-apps" />
        <NavButton icon={BadgeAlertIcon} label={t("nav.alarms")} active={view === "alarms"} onClick={() => onNavigate("alarms")} testId="nav-alarms" />
        <NavButton icon={HistoryIcon} label={t("nav.backups")} active={view === "backups"} onClick={() => onNavigate("backups")} testId="nav-backups" />
        <NavButton icon={ShieldCheckIcon} label={t("nav.audit")} active={view === "audit"} onClick={() => onNavigate("audit")} testId="nav-audit" />
        <NavButton icon={SettingsIcon} label={t("nav.settings")} active={view === "settings"} onClick={() => onNavigate("settings")} testId="nav-settings" />
      </nav>

      <p className="foot mono" data-testid="rail-version">
        {t("rail.version", { version: version ? versionLabel(version.version) : "…" })}
      </p>
    </aside>
  );
}

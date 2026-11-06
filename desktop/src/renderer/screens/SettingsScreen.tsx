import { useMemo } from "react";

import { THEMES } from "../../shared/appearance.ts";
import { useAppearance } from "../appearance/AppearanceProvider.tsx";
import type { Profile } from "../bridge.ts";
import { FontPicker } from "../components/FontPicker.tsx";
import { Segmented } from "../components/Segmented.tsx";
import { SetRow } from "../components/SetRow.tsx";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { formatCount, versionLabel } from "../lib/format.ts";
import { nearestWeight, weightsOf } from "../lib/fonts.ts";
import { LANGUAGES } from "../lib/languages.ts";
import type { VersionPayload } from "../lib/types.ts";

interface Props {
  profiles: readonly Profile[];
  version: VersionPayload | null;
  onRemoveProfile: (target: string) => void;
}

export function SettingsScreen({ profiles, version, onRemoveProfile }: Props) {
  const t = useT();
  const language = useLanguage();
  const { appearance: a, theme, glassAvailable, families, fontsListed, update, reset } = useAppearance();
  const names = useMemo(() => [...families.keys()].sort((x, y) => x.localeCompare(y)), [families]);
  const uiFaces = families.get(a.uiFont.family) ?? [];
  const codeFaces = families.get(a.codeFont.family) ?? [];

  return (
    <div className="screen" data-testid="screen-settings">
      <header className="stage-head">
        <h1 className="title">{t("settings.title")}</h1>
        <p className="meta">{t("settings.appearance_meta")}</p>
      </header>

      <section className="panel">
        <div className="panel-head"><h2 className="section-title">{t("settings.theme")}</h2></div>
        <div className="well">
          <SetRow title={t("settings.theme")} hint={t(glassAvailable ? "settings.theme_hint" : "settings.theme_hint_no_glass")}>
            <Segmented
              label={t("settings.theme")}
              value={theme}
              options={THEMES.map((name) => ({ value: name, label: t(`theme.${name}`), testId: `theme-${name}` }))}
              onChange={(next) => update({ theme: next })}
            />
          </SetRow>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head"><h2 className="section-title">{t("settings.language")}</h2></div>
        <div className="well">
          <SetRow title={t("settings.language")} hint={t("settings.language_hint")}>
            <Segmented
              label={t("settings.language")}
              value={a.language}
              options={LANGUAGES.map((l) => ({ value: l.code, label: l.name, testId: `lang-${l.code}` }))}
              onChange={(lang) => update({ language: lang })}
            />
          </SetRow>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head"><h2 className="section-title">{t("settings.fonts")}</h2></div>
        <div className="well">
          <SetRow title={t("settings.ui_font")} hint={t("settings.ui_font_hint")}>
            <FontPicker
              families={names}
              family={a.uiFont.family}
              weight={nearestWeight(uiFaces, a.uiFont.weight)}
              weights={weightsOf(uiFaces)}
              defaultLabel={t("font.default")}
              label={t("settings.ui_font")}
              weightLabel={t("font.weight_of", { font: t("settings.ui_font") })}
              onFamily={(family) => update({ uiFont: { family, weight: nearestWeight(families.get(family) ?? [], 400) } })}
              onWeight={(weight) => update({ uiFont: { family: a.uiFont.family, weight } })}
              testId="ui-font"
            />
          </SetRow>
          <SetRow title={t("settings.code_font")} hint={t("settings.code_font_hint")}>
            <FontPicker
              families={names}
              family={a.codeFont.family}
              weight={nearestWeight(codeFaces, a.codeFont.weight)}
              weights={weightsOf(codeFaces)}
              defaultLabel={t("font.default")}
              label={t("settings.code_font")}
              weightLabel={t("font.weight_of", { font: t("settings.code_font") })}
              onFamily={(family) => update({ codeFont: { family, weight: nearestWeight(families.get(family) ?? [], 400) } })}
              onWeight={(weight) => update({ codeFont: { family: a.codeFont.family, weight } })}
              testId="code-font"
            />
          </SetRow>
          <p className="font-note" data-testid="font-note">
            {fontsListed
              ? t("font.families", { count: names.length, n: formatCount(names.length, language) })
              : t("font.unavailable")}
          </p>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head"><h2 className="section-title">{t("settings.servers")}</h2></div>
        <div className="well">
          <SetRow title={t("settings.servers")} hint={t("settings.servers_hint")}>
            {profiles.length === 0 ? (
              <span className="empty">{t("settings.no_servers")}</span>
            ) : (
              <ul className="server-list">
                {profiles.map((p) => (
                  <li key={p.target}>
                    <span className="mono">{p.target}</span>
                    <button type="button" className="btn btn-ghost" onClick={() => onRemoveProfile(p.target)}
                      aria-label={t("settings.remove_server", { target: p.target })}
                      data-testid={`remove-${p.target}`}
                    >
                      {t("settings.remove")}
                    </button>
                  </li>
                ))}
              </ul>
            )}
          </SetRow>
        </div>
      </section>

      <section className="panel">
        <div className="panel-head"><h2 className="section-title">{t("settings.about")}</h2></div>
        <div className="well">
          <SetRow title={t("settings.about")} hint={t("settings.about_hint")}>
            <span className="about mono" data-testid="about">
              {t("about.line", { version: version ? versionLabel(version.version) : "…", protocol: version ? version.protocol : "…" })}
            </span>
          </SetRow>
        </div>
      </section>

      <div className="set-foot">
        <button type="button" className="btn btn-ghost" onClick={reset} data-testid="reset">
          {t("settings.reset")}
        </button>
      </div>
    </div>
  );
}

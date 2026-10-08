import { useMemo } from "react";

import { useAppearance } from "../appearance/AppearanceProvider.tsx";
import type { Profile } from "../bridge.ts";
import { FontPicker } from "../components/FontPicker.tsx";
import { Segmented } from "../components/Segmented.tsx";
import { RangeRow, SetRow } from "../components/SetRow.tsx";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { formatCount, versionLabel } from "../lib/format.ts";
import { nearestWeight, weightsOf } from "../lib/fonts.ts";
import { LANGUAGES } from "../lib/languages.ts";
import type { VersionPayload } from "../lib/types.ts";
import { ACCENTS, LIMITS, TONES } from "../../shared/appearance.ts";

interface Props {
  profiles: readonly Profile[];
  version: VersionPayload | null;
  onRemoveProfile: (target: string) => void;
}

export function SettingsScreen({ profiles, version, onRemoveProfile }: Props) {
  const t = useT();
  const language = useLanguage();
  const { appearance: a, modes, families, fontsListed, update, reset } = useAppearance();
  const names = useMemo(() => [...families.keys()].sort((x, y) => x.localeCompare(y)), [families]);
  const uiFaces = families.get(a.uiFont.family) ?? [];
  const codeFaces = families.get(a.codeFont.family) ?? [];

  return (
    <div className="screen" data-testid="screen-settings">
      <header className="stage-head">
        <p className="eyebrow">{t("settings.title")}</p>
        <h1 className="title">{t("settings.appearance")}</h1>
        <p className="meta">{t("settings.appearance_meta")}</p>
      </header>

      <section className="panel glass settings">
        <SetRow title={t("settings.window")} hint={t("settings.window_hint")}>
          <Segmented
            label={t("settings.window")}
            value={a.mode}
            options={modes.map((m) => ({ value: m, label: t(`mode.${m}`), testId: `mode-${m}` }))}
            onChange={(mode) => update({ mode })}
          />
        </SetRow>
        <RangeRow title={t("settings.tint")} hint={t("settings.tint_hint")} id="tint" value={a.tint} min={LIMITS.tint[0]} max={LIMITS.tint[1]} onChange={(tint) => update({ tint })} />
        <RangeRow title={t("settings.panel")} hint={t("settings.panel_hint")} id="panel" value={a.panel} min={LIMITS.panel[0]} max={LIMITS.panel[1]} onChange={(panel) => update({ panel })} />
        <RangeRow title={t("settings.edge")} hint={t("settings.edge_hint")} id="edge" value={a.edge} min={LIMITS.edge[0]} max={LIMITS.edge[1]} onChange={(edge) => update({ edge })} />
        <RangeRow title={t("settings.glow")} hint={t("settings.glow_hint")} id="glow" value={a.glow} min={LIMITS.glow[0]} max={LIMITS.glow[1]} onChange={(glow) => update({ glow })} />
        <SetRow title={t("settings.accent")} hint={t("settings.accent_hint")}>
          <Segmented
            label={t("settings.accent")}
            value={a.accent}
            options={ACCENTS.map((x) => ({ value: x, label: t(`accent.${x}`), testId: `accent-${x}` }))}
            onChange={(accent) => update({ accent })}
          />
        </SetRow>
        <SetRow title={t("settings.theme")} hint={t("settings.theme_hint")}>
          <Segmented
            label={t("settings.theme")}
            value={a.tone}
            options={TONES.map((x) => ({ value: x, label: t(`tone.${x}`), testId: `tone-${x}` }))}
            onChange={(tone) => update({ tone })}
          />
        </SetRow>
        <SetRow title={t("settings.ui_font")} hint={t("settings.ui_font_hint")}>
          <FontPicker
            families={names}
            family={a.uiFont.family}
            weight={nearestWeight(uiFaces, a.uiFont.weight)}
            weights={weightsOf(uiFaces)}
            defaultLabel={t("font.system")}
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
            defaultLabel={t("font.bundled")}
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
      </section>

      <section className="panel glass settings">
        <SetRow title={t("settings.language")} hint={t("settings.language_hint")}>
          <Segmented
            label={t("settings.language")}
            value={a.language}
            options={LANGUAGES.map((l) => ({ value: l.code, label: l.name, testId: `lang-${l.code}` }))}
            onChange={(lang) => update({ language: lang })}
          />
        </SetRow>
      </section>

      <section className="panel glass settings">
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
      </section>

      <section className="panel glass settings">
        <SetRow title={t("settings.about")} hint={t("settings.about_hint")}>
          <span className="about mono" data-testid="about">
            {t("about.line", { version: version ? versionLabel(version.version) : "…", protocol: version ? version.protocol : "…" })}
          </span>
        </SetRow>
      </section>

      <div className="set-foot">
        <button type="button" className="btn btn-ghost" onClick={reset} data-testid="reset">
          {t("settings.reset")}
        </button>
      </div>
    </div>
  );
}

import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";

/** Kapat, küçült, büyüt. macOS'ta sistemin kendi ışıkları var, biz çizmeyiz. */
export function WindowLights() {
  const t = useT();
  if (bridge.platform === "darwin") return <div className="lights is-mac" />;
  return (
    <div className="lights">
      <button type="button" className="light light-close" aria-label={t("window.close")} title={t("window.close")} onClick={() => void bridge.windowControl("close")} />
      <button type="button" className="light light-min" aria-label={t("window.minimize")} title={t("window.minimize")} onClick={() => void bridge.windowControl("minimize")} />
      <button type="button" className="light light-max" aria-label={t("window.maximize")} title={t("window.maximize")} onClick={() => void bridge.windowControl("maximize")} />
    </div>
  );
}

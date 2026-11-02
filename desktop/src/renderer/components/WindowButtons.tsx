import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";

/** Küçült, büyüt, kapat. macOS'ta sistemin kendi ışıkları var, biz çizmeyiz. */
export function WindowButtons() {
  const t = useT();
  if (bridge.platform === "darwin") return null;
  return (
    <div className="win-btns">
      <button type="button" className="win-btn" aria-label={t("window.minimize")} title={t("window.minimize")} onClick={() => void bridge.windowControl("minimize")}>
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="M0 5h10" stroke="currentColor" /></svg>
      </button>
      <button type="button" className="win-btn" aria-label={t("window.maximize")} title={t("window.maximize")} onClick={() => void bridge.windowControl("maximize")}>
        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" aria-hidden="true"><rect x=".5" y=".5" width="9" height="9" stroke="currentColor" /></svg>
      </button>
      <button type="button" className="win-btn win-close" aria-label={t("window.close")} title={t("window.close")} onClick={() => void bridge.windowControl("close")}>
        <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d="m0 0 10 10M10 0 0 10" stroke="currentColor" /></svg>
      </button>
    </div>
  );
}

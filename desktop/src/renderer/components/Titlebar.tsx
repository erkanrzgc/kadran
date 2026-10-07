import { bridge } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";

/**
 * Pencerenin başlık çubuğu. Sürükleme alanı CSS'te (-webkit-app-region).
 * macOS'ta sistem trafik ışıkları kalır; düğmeleri biz çizmeyiz. Logo
 * yeri (.mark) tasarımcının logosu gelene kadar boş.
 */
export function Titlebar() {
  const t = useT();
  const mac = bridge.platform === "darwin";
  return (
    <header className={mac ? "titlebar is-mac" : "titlebar"}>
      <div className="tb-brand">
        <span className="mark" aria-hidden="true" />
        <span className="wordmark">KΛDRΛN</span>
      </div>
      <div className="tb-drag" />
      {mac ? null : (
        <div className="tb-controls">
          <button type="button" className="tb-btn" aria-label={t("window.minimize")} onClick={() => void bridge.windowControl("minimize")}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M2 6h8" /></svg>
          </button>
          <button type="button" className="tb-btn" aria-label={t("window.maximize")} onClick={() => void bridge.windowControl("maximize")}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><rect x="2.5" y="2.5" width="7" height="7" rx="1.5" /></svg>
          </button>
          <button type="button" className="tb-btn tb-close" aria-label={t("window.close")} onClick={() => void bridge.windowControl("close")}>
            <svg viewBox="0 0 12 12" aria-hidden="true"><path d="M3 3l6 6M9 3l-6 6" /></svg>
          </button>
        </div>
      )}
    </header>
  );
}

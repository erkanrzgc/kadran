import { useEffect } from "react";

import { useT } from "../i18n/I18nProvider.tsx";
import { useToastState } from "./ToastProvider.tsx";

const TOAST_MS = 9_000;

/**
 * Bildirim. Sunucudan gelen hata metni olduğu gibi gösterilir; her zaman
 * düz metin (React kaçırarak yazar). Uzun metin satır kırar (controls.css).
 */
export function Toast() {
  const t = useT();
  const { message, dismiss } = useToastState();

  useEffect(() => {
    if (!message) return;
    const timer = setTimeout(dismiss, TOAST_MS);
    return () => clearTimeout(timer);
  }, [message, dismiss]);

  if (!message) return null;
  return (
    <div className="toast glass" role="status" aria-live="polite" data-testid="toast">
      <p className="toast-text">{t(message.key, message.params)}</p>
      <button type="button" className="toast-close" aria-label={t("toast.dismiss")} onClick={dismiss}>
        ×
      </button>
    </div>
  );
}

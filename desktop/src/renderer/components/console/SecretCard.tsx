import { useT } from "../../i18n/I18nProvider.tsx";

interface Props {
  /** Kart içeriği; kapalıyken null: o zaman hiçbir şey çizilmez, metin DOM'da kalmaz. */
  content: string | null;
  onClose: () => void;
  /** Panelin üstünde kalan yer (px): kart pencerenin üstüne taşmasın, gövdesi kayar. */
  maxHeight: number;
}

/**
 * Gizli çıktı kartı (totp enable). Metin olduğu gibi, eş aralıklı ve satır
 * aralığı 1 ile gösterilir: CLI'ın metin QR'ı taranabilir kalır. Seçilemez,
 * kopyalanamaz; hiçbir yere yazılmaz (lib/consoleSession.ts).
 */
export function SecretCard({ content, onClose, maxHeight }: Props) {
  const t = useT();
  if (content === null) return null;
  return (
    <section className="console-card" role="dialog" aria-label={t("console.card_title")} data-testid="console-card" style={{ maxHeight }}>
      <header className="console-card-head">
        <strong>{t("console.card_title")}</strong>
        <span>{t("console.card_hint")}</span>
      </header>
      <pre className="console-card-body" data-testid="console-card-body" onCopy={(e) => e.preventDefault()} onContextMenu={(e) => e.preventDefault()}>
        {content}
      </pre>
      <div className="console-card-foot">
        <button type="button" className="btn btn-primary" onClick={onClose} data-testid="console-card-saved">
          {t("console.card_saved")}
        </button>
      </div>
    </section>
  );
}

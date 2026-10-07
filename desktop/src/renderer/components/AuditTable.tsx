import { useEffect, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { outcomeView } from "../lib/chain.ts";
import { errorText } from "../lib/errors.ts";
import { describeActor, formatTime } from "../lib/format.ts";
import type { AuditRecord } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

const COPIED_MS = 1_500;

/** Denetim tablosu. Parmak izine tıklayınca tamamı panoya kopyalanır. */
export function AuditTable({ records, testId }: { records: readonly AuditRecord[]; testId: string }) {
  const t = useT();
  const language = useLanguage();
  const toast = useToast();
  const [copied, setCopied] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Bileşen kapanırken bekleyen "kopyalandı" zamanlayıcısı bırakılmaz.
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const copy = async (fp: string) => {
    try {
      await bridge.copyText(fp);
      setCopied(fp);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(null), COPIED_MS);
    } catch (err) {
      toast("toast.copy_failed", { error: errorText(err) });
    }
  };

  return (
    <div className="table-wrap">
      <table className="audit-table" data-testid={testId}>
        <thead>
          <tr>
            <th scope="col">{t("audit.col_seq")}</th>
            <th scope="col">{t("audit.col_time")}</th>
            <th scope="col">{t("audit.col_actor")}</th>
            <th scope="col">{t("audit.col_action")}</th>
            <th scope="col">{t("audit.col_target")}</th>
            <th scope="col">{t("audit.col_outcome")}</th>
          </tr>
        </thead>
        <tbody>
          {records.map((r) => {
            const fp = r.actor?.ssh_key_fingerprint ?? "";
            const actor = describeActor(r.actor) ?? t("audit.unknown_actor");
            const out = outcomeView(r.outcome);
            return (
              <tr key={r.seq} data-testid="audit-row" data-seq={r.seq}>
                <td className="mono">{r.seq}</td>
                <td className="mono">{formatTime(r.ts, language)}</td>
                <td className="mono">
                  {fp ? (
                    <button type="button" className="fp" title={t("audit.copy")} onClick={() => void copy(fp)}>
                      {copied === fp ? t("audit.copied") : actor}
                    </button>
                  ) : (
                    actor
                  )}
                </td>
                <td className="mono">{r.action || "—"}</td>
                <td>{r.target || "—"}</td>
                <td className={`outcome is-${out.tone}`}>{out.label}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

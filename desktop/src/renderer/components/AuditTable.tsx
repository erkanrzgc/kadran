import { Fragment, useEffect, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { outcomeView } from "../lib/chain.ts";
import { errorText } from "../lib/errors.ts";
import { describeActor, formatTime } from "../lib/format.ts";
import type { AuditRecord } from "../lib/types.ts";
import { useToast } from "../toast/ToastProvider.tsx";

const COPIED_MS = 1_500;

/** Denetim tablosu. Parmak izine tıklayınca tamamı panoya kopyalanır. */
export function AuditTable({ records, testId, placeholder }: { records: readonly AuditRecord[]; testId: string; placeholder?: string }) {
  const t = useT();
  const language = useLanguage();
  const toast = useToast();
  const [copied, setCopied] = useState<string | null>(null);
  const [expanded, setExpanded] = useState<string | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Bileşen kapanırken bekleyen "kopyalandı" zamanlayıcısı bırakılmaz.
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const copy = async (fp: string, seq: string) => {
    try {
      await bridge.copyText(fp);
      setCopied(seq);
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
          {records.length === 0 && placeholder ? (
            <tr data-testid="audit-placeholder">
              <td colSpan={6} className="audit-placeholder">
                {placeholder}
              </td>
            </tr>
          ) : null}
          {records.map((r) => {
            const fp = r.actor?.ssh_key_fingerprint ?? "";
            const actor = describeActor(r.actor) ?? t("audit.unknown_actor");
            const out = outcomeView(r.outcome);
            return (
              <Fragment key={r.seq}><tr data-testid="audit-row" data-seq={r.seq}>
                <td className="mono">{r.seq}</td>
                <td className="mono">{formatTime(r.ts, language)}</td>
                <td className="mono">
                  {fp ? (
                    <button type="button" className="fp" title={t("audit.copy")} onClick={() => void copy(fp, r.seq)}>
                      {copied === r.seq ? t("audit.copied") : actor}
                    </button>
                  ) : (
                    actor
                  )}
                </td>
                <td className="mono"><button className="audit-detail-button" type="button" aria-expanded={expanded === r.seq} onClick={() => setExpanded((value) => value === r.seq ? null : r.seq)} title={t("audit.show_detail")} data-testid={`audit-detail-${r.seq}`}>{r.action || "—"}</button></td>
                <td>{r.target || "—"}</td>
                <td className={`outcome is-${out.tone}`}>{out.label}</td>
              </tr>{expanded === r.seq ? <tr data-testid="audit-detail-row"><td colSpan={6}><div className="audit-detail"><dl className="rows"><div className="row"><dt>{t("audit.detail")}</dt><dd className="operation-message">{r.detail || "—"}</dd></div><div className="row"><dt>{t("audit.source")}</dt><dd className="mono">{r.source || "—"}</dd></div><div className="row"><dt>{t("audit.fingerprint")}</dt><dd className="mono">{r.actor?.ssh_key_fingerprint || "—"}</dd></div><div className="row"><dt>{t("audit.source_ip")}</dt><dd className="mono">{r.actor?.source_ip || "—"}</dd></div><div className="row"><dt>{t("audit.origin")}</dt><dd>{r.actor?.origin || "—"}</dd></div></dl><h3 className="label">{t("audit.parameters")}</h3><pre className="log-output mono">{r.params_json || "—"}</pre></div></td></tr> : null}</Fragment>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

import { useEffect, useRef, useState } from "react";

import { useT } from "../i18n/I18nProvider.tsx";
import { confirmationMatches } from "../lib/management.ts";

export type AppAction = "deploy" | "rollback" | "delete";

export function OperationDialog({ action, appId, branch, pending, error, notice, onConfirm, onClose }: { action: AppAction; appId: string; branch: string; pending: boolean; error: string | null; notice: string | null; onConfirm: (ref: string, confirmation: string) => void; onClose: () => void }) {
  const t = useT();
  const dialog = useRef<HTMLDialogElement>(null);
  const confirmationInput = useRef<HTMLInputElement>(null);
  const [confirmation, setConfirmation] = useState("");
  const [ref, setRef] = useState(branch || "main");
  useEffect(() => {
    const el = dialog.current;
    const previous = document.activeElement;
    el?.showModal();
    confirmationInput.current?.focus();
    return () => { el?.close(); if (previous instanceof HTMLElement && previous.isConnected) previous.focus(); };
  }, []);
  return (
    <dialog ref={dialog} className="operation-dialog glass" aria-labelledby="operation-title" onCancel={(event) => { event.preventDefault(); if (!pending) onClose(); }} data-testid="operation-dialog">
      <form onSubmit={(event) => { event.preventDefault(); if (!pending && confirmationMatches(appId, confirmation) && (action !== "deploy" || ref.trim())) onConfirm(ref.trim(), confirmation); }}>
        <p className="eyebrow">{t("operation.review")}</p>
        <h2 id="operation-title" className="title">{t(`operation.${action}_title`, { app: appId })}</h2>
        <p className="meta">{t(`operation.${action}_body`)}</p>
        {action === "deploy" ? <label className="form-field">{t("operation.ref")}<input className="field mono" value={ref} disabled={pending} onChange={(event) => setRef(event.target.value)} data-testid="deploy-ref" /></label> : null}
        <label className="form-field">{t("operation.confirm_name", { app: appId })}<input ref={confirmationInput} className="field mono" autoComplete="off" value={confirmation} disabled={pending} onChange={(event) => setConfirmation(event.target.value)} data-testid="operation-confirmation" /></label>
        {error ? <p className="operation-message is-bad" role="alert" data-testid="operation-error">{error}</p> : null}
        {notice ? <p className="scope-note" role="status" data-testid="operation-notice-dialog">{notice}</p> : null}
        <p className="meta" role="status">{pending ? t("operation.running") : t("operation.review_hint")}</p>
        <div className="form-actions">
          <button className="btn btn-ghost" type="button" disabled={pending} onClick={onClose} data-testid="operation-cancel">{t("action.cancel")}</button>
          <button className={action === "delete" ? "btn btn-danger" : "btn btn-primary"} type="submit" disabled={pending || !confirmationMatches(appId, confirmation) || (action === "deploy" && !ref.trim())} data-testid="operation-submit">{pending ? t("operation.running") : t(`action.${action}`)}</button>
        </div>
      </form>
    </dialog>
  );
}

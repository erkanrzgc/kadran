import { useEffect, useRef, useState } from "react";

import { useT } from "../i18n/I18nProvider.tsx";
import { DISCONNECT_WORD, canDisconnect } from "../lib/github.ts";

interface Props {
  pending: boolean;
  error: string | null;
  onConfirm: () => void;
  onClose: () => void;
}

/**
 * GitHub bağlantısını kesme onayı. Düğme yalnız TAM "disconnect" yazılınca
 * açılır; ana süreçteki yerel kutu bunun ÜSTÜNE ayrıca sorar.
 */
export function GitHubDisconnectDialog({ pending, error, onConfirm, onClose }: Props) {
  const t = useT();
  const dialog = useRef<HTMLDialogElement>(null);
  const input = useRef<HTMLInputElement>(null);
  const [typed, setTyped] = useState("");
  useEffect(() => {
    const el = dialog.current;
    const previous = document.activeElement;
    el?.showModal();
    input.current?.focus();
    return () => {
      el?.close();
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, []);
  const ready = canDisconnect(typed);
  return (
    <dialog ref={dialog} className="operation-dialog" aria-labelledby="github-disconnect-title" onCancel={(event) => { event.preventDefault(); if (!pending) onClose(); }} data-testid="github-disconnect-dialog">
      <form onSubmit={(event) => { event.preventDefault(); if (!pending && ready) onConfirm(); }}>
        <p className="eyebrow">{t("operation.review")}</p>
        <h2 id="github-disconnect-title" className="title">{t("github.disconnect_title")}</h2>
        <p className="meta">{t("github.disconnect_body")}</p>
        <label className="form-field">{t("github.disconnect_confirm", { word: DISCONNECT_WORD })}<input ref={input} className="field mono" autoComplete="off" value={typed} disabled={pending} onChange={(event) => setTyped(event.target.value)} data-testid="github-disconnect-confirmation" /></label>
        {error ? <p className="operation-message is-bad" role="alert" data-testid="github-disconnect-error">{error}</p> : null}
        <div className="form-actions">
          <button className="btn btn-ghost" type="button" disabled={pending} onClick={onClose} data-testid="github-disconnect-cancel">{t("action.cancel")}</button>
          <button className="btn btn-danger" type="submit" disabled={pending || !ready} data-testid="github-disconnect-submit">{pending ? t("operation.running") : t("github.disconnect")}</button>
        </div>
      </form>
    </dialog>
  );
}

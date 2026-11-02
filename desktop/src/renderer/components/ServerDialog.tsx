import { useEffect, useRef, useState, type FormEvent } from "react";

import type { Profile } from "../bridge.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { PROVIDERS } from "../lib/providers.ts";

export interface ServerDraft {
  target: string;
  name: string;
  provider: string;
}

interface Props {
  /** Düzenlenen sunucu; yoksa yeni sunucu eklenir. */
  profile: Profile | null;
  pending: boolean;
  /** Eklenemedi: sebep kutunun içinde görünür. */
  error: string | null;
  onSubmit: (draft: ServerDraft) => void;
  onClose: () => void;
}

/** "Sunucu ekle" ve kart ayarları. Yeni sunucuya önce bağlanılır, sonra kaydedilir. */
export function ServerDialog({ profile, pending, error, onSubmit, onClose }: Props) {
  const t = useT();
  const dialog = useRef<HTMLDialogElement>(null);
  const [target, setTarget] = useState(profile?.target ?? "");
  const [name, setName] = useState(profile?.name ?? "");
  const [provider, setProvider] = useState(profile?.provider ?? "");

  useEffect(() => {
    const el = dialog.current;
    el?.showModal();
    return () => el?.close();
  }, []);

  const value = target.trim();
  const submit = (event: FormEvent) => {
    event.preventDefault();
    if (pending || !value) return;
    onSubmit({ target: value, name: name.trim() || value, provider });
  };
  // Elle düzenlenmiş dosyadaki bilinmeyen etiket seçimden düşmesin.
  const options = provider && !PROVIDERS.has(provider) ? [provider, ...PROVIDERS.keys()] : [...PROVIDERS.keys()];

  return (
    <dialog ref={dialog} className="operation-dialog" aria-labelledby="server-dialog-title" onCancel={(event) => { event.preventDefault(); if (!pending) onClose(); }} data-testid="server-dialog">
      <form onSubmit={submit}>
        <h2 id="server-dialog-title" className="title">{profile ? t("fleet.edit_title") : t("fleet.add_title")}</h2>
        <p className="meta">{profile ? t("fleet.edit_body") : t("rail.hint")}</p>
        {profile ? null : (
          <label className="form-field">{t("rail.target_label")}
            <input className="field mono" value={target} disabled={pending} onChange={(e) => setTarget(e.target.value)} placeholder={t("rail.target_placeholder")} spellCheck={false} autoComplete="off" autoFocus data-testid="target-input" />
          </label>
        )}
        <label className="form-field">{t("fleet.name")}
          <input className="field" value={name} disabled={pending} onChange={(e) => setName(e.target.value)} placeholder={value} spellCheck={false} autoComplete="off" data-testid="server-name" />
        </label>
        <label className="form-field">{t("fleet.provider")}
          <select className="sel" value={provider} disabled={pending} onChange={(e) => setProvider(e.target.value)} data-testid="server-provider">
            <option value="">{t("fleet.no_provider")}</option>
            {options.map((key) => <option value={key} key={key}>{PROVIDERS.get(key)?.name ?? key}</option>)}
          </select>
        </label>
        {profile ? null : <p className="scope-note">{t("setup.compact_help")}</p>}
        {error ? <p className="dialog-error" role="alert" data-testid="server-error">{error}</p> : null}
        <div className="form-actions">
          <button className="btn btn-ghost" type="button" disabled={pending} onClick={onClose} data-testid="server-cancel">{t("action.cancel")}</button>
          <button className="btn btn-primary" type="submit" disabled={pending || !value} data-testid="connect">{pending ? t("rail.connecting") : profile ? t("fleet.save") : t("rail.connect")}</button>
        </div>
      </form>
    </dialog>
  );
}

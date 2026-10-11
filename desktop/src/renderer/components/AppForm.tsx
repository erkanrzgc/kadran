import { useState, type FormEvent } from "react";

import { useT } from "../i18n/I18nProvider.tsx";
import { newAppDraft, specToDraft, validateAppDraft, type AppDraft } from "../lib/management.ts";
import type { AppSpec } from "../lib/types.ts";

export function AppForm({ original, pending, error, onSave, onCancel }: { original?: AppSpec; pending: boolean; error: string | null; onSave: (draft: AppDraft) => void; onCancel?: () => void }) {
  const t = useT();
  const [draft, setDraft] = useState<AppDraft>(() => original ? specToDraft(original) : newAppDraft());
  const [submitted, setSubmitted] = useState(false);
  const validation = validateAppDraft(draft, !!original);
  const field = (key: keyof AppDraft, label: string, immutable = false, numeric = false) => (
    <label className="form-field" key={key}>{t(label)}
      <input className={numeric ? "field" : "field mono"} value={draft[key]} disabled={pending || (!!original && immutable)} inputMode={numeric ? "numeric" : undefined} spellCheck={false} autoComplete="off" onChange={(event) => setDraft((prev) => ({ ...prev, [key]: event.target.value }))} data-testid={`app-${key}`} />
    </label>
  );
  const submit = (event: FormEvent) => { event.preventDefault(); setSubmitted(true); if (!validation && !pending) onSave(draft); };
  return (
    <form className="app-form" onSubmit={submit} data-testid="app-form">
      <div className="form-grid">
        {field("appId", "form.app_id", true)}{field("domain", "form.domain")}
        {field("gitHost", "form.git_host", true)}{field("gitOwner", "form.git_owner", true)}
        {field("gitRepo", "form.git_repo", true)}{field("gitBranch", "form.branch")}
        {field("dockerfilePath", "form.dockerfile", true)}{field("containerPort", "form.port", true, true)}
        {field("replicas", "form.replicas", false, true)}{field("healthPath", "form.health")}
        {!original ? <>{field("memoryMiB", "form.memory", false, true)}{field("cpuMillis", "form.cpu", false, true)}{field("blkioWeight", "form.io", false, true)}</> : null}
      </div>
      <p className="scope-note">{original ? t("form.update_scope") : t("form.create_scope")}</p>
      {original ? <section className="environment-names"><h3 className="label">{t("form.existing_env")}</h3><p className="mono operation-message" data-testid="app-existing-env">{Object.keys(original.env).join(", ") || t("form.no_env")}</p><p className="scope-note">{t("form.env_preserved")}</p></section> : null}
      <div className="form-grid">
        <label className="form-field">{t("form.env")}<textarea className="field mono" rows={4} value={draft.envText} disabled={pending} spellCheck={false} onChange={(event) => setDraft((prev) => ({ ...prev, envText: event.target.value }))} data-testid="app-envText" /><span className="hint">{t("form.env_hint")}</span></label>
        <label className="form-field">{t("form.volumes")}<textarea className="field mono" rows={4} value={draft.volumesText} disabled={pending} spellCheck={false} onChange={(event) => setDraft((prev) => ({ ...prev, volumesText: event.target.value }))} data-testid="app-volumesText" /><span className="hint">{t("form.volumes_hint")}</span></label>
      </div>
      {original ? <label className="form-field">{t("form.env_remove")}<textarea className="field mono" rows={2} value={draft.envRemoveText} disabled={pending} spellCheck={false} onChange={(event) => setDraft((prev) => ({ ...prev, envRemoveText: event.target.value }))} data-testid="app-envRemoveText" /><span className="hint">{t("form.env_remove_hint")}</span></label> : null}
      <p className="scope-note">{t("form.volumes_scope")}</p>
      {submitted && validation ? <p className="operation-message is-bad" role="alert" data-testid="form-validation">{t(validation)}</p> : null}
      {error ? <p className="operation-message is-bad" role="alert" data-testid="form-error">{error}</p> : null}
      <div className="form-actions">
        {onCancel ? <button type="button" className="btn btn-ghost" disabled={pending} onClick={onCancel}>{t("action.cancel")}</button> : null}
        <button type="submit" className="btn btn-primary" disabled={pending} data-testid="app-save">{pending ? t("operation.running") : original ? t("action.save") : t("action.create")}</button>
      </div>
    </form>
  );
}

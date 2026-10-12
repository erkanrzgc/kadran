import { useCallback, useState } from "react";

import { bridge } from "../bridge.ts";
import { AppForm } from "../components/AppForm.tsx";
import { EmptyState } from "../components/EmptyState.tsx";
import { OperationDialog, type AppAction } from "../components/OperationDialog.tsx";
import { useManagementOperation } from "../hooks/useManagementOperation.ts";
import { useManagementQuery } from "../hooks/useManagementQuery.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { isCancelled } from "../../shared/management.ts";
import { formatTime } from "../lib/format.ts";
import type { Params } from "../lib/i18n.ts";
import { canDeleteApp, draftToSpec, editableUpdate, type AppDraft } from "../lib/management.ts";
import type { AppDeletePayload } from "../lib/types.ts";

type Tab = "deployments" | "logs" | "configuration";
interface Message { key: string; params?: Params; detail?: string; output?: string; truncated?: boolean }

export function AppsScreen({ target }: { target: string | null }) {
  const t = useT();
  const [selected, setSelected] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleted, setDeleted] = useState<AppDeletePayload | null>(null);
  if (!target) return <EmptyState title={t("apps.no_target")} body={t("apps.connect_first")} />;
  if (selected) return <AppDetail key={`${target}:${selected}`} target={target} appId={selected} onBack={() => setSelected(null)} onDeleted={(result) => { setDeleted(result); setSelected(null); }} />;
  return <AppsList target={target} creating={creating} deleted={deleted} onCreate={setCreating} onSelect={(id) => { setCreating(false); setDeleted(null); setSelected(id); }} />;
}

function AppsList({ target, creating, deleted, onCreate, onSelect }: { target: string; creating: boolean; deleted: AppDeletePayload | null; onCreate: (value: boolean) => void; onSelect: (id: string) => void }) {
  const t = useT();
  const load = useCallback(() => bridge.appsList(target), [target]);
  const query = useManagementQuery(load);
  const operation = useManagementOperation();
  const create = async (draft: AppDraft) => {
    const result = await operation.run(() => bridge.appCreate(target, draftToSpec(draft)));
    if (result?.app?.spec) onSelect(result.app.spec.app_id);
  };
  return (
    <div className="screen" data-testid="screen-apps">
      <header className="stage-head resource-head"><div><p className="eyebrow">{t("apps.eyebrow")}</p><h1 className="title">{t("apps.title")}</h1><p className="meta">{t("apps.subtitle")}</p></div><button type="button" className="btn btn-primary" disabled={operation.pending || creating} onClick={() => { operation.clearError(); onCreate(true); }} data-testid="app-new">{t("apps.new")}</button></header>
      {operation.notice ? <p className="scope-note" role="status" data-testid="operation-notice">{operation.notice}</p> : null}
      {deleted ? <section className="panel glass" role="status" data-testid="app-deleted"><p className="is-ok">{t("operation.deleted", { app: deleted.app_id, containers: deleted.containers_removed, releases: deleted.releases_deleted, deployments: deleted.deployments_deleted })}</p><p className="scope-note">{t("operation.volumes_kept", { names: deleted.volumes_kept.join(", ") || t("value.none") })}</p></section> : null}
      {creating ? <section className="panel glass"><h2 className="section-title">{t("apps.new")}</h2><AppForm pending={operation.pending} error={operation.error} onSave={(draft) => void create(draft)} onCancel={() => onCreate(false)} /></section> : null}
      <section className="panel glass">
        <div className="panel-head"><h2 className="section-title">{t("apps.resources")}</h2><button type="button" className="btn btn-ghost" disabled={query.loading || operation.pending} onClick={() => void query.reload()} data-testid="apps-refresh">{t("status.refresh")}</button></div>
        {query.error ? <p className="operation-message is-bad" role="alert" data-testid="apps-error">{query.error}</p> : null}
        {query.loading ? <p className="meta" role="status">{t("resource.loading")}</p> : query.data?.apps.length === 0 ? <div className="resource-empty"><h3>{t("apps.empty_title")}</h3><p className="meta">{t("apps.empty_body")}</p></div> : null}
        <div className="resource-grid">
          {query.data?.apps.map((app) => app.spec ? <button type="button" className="resource-card glass" key={app.spec.app_id} onClick={() => onSelect(app.spec!.app_id)} data-testid={`app-card-${app.spec.app_id}`}>
            <span className="resource-mark" aria-hidden="true">◇</span><span className="resource-card-title mono">{app.spec.app_id}</span><span className="meta mono">{app.spec.git_owner}/{app.spec.git_repo}</span><span className="resource-card-domain">{app.spec.domain || t("apps.no_domain")}</span><span className="resource-card-foot"><span>{t("apps.branch", { branch: app.spec.git_branch })}</span><span>{t("apps.releases", { count: app.release_count })}</span></span>
          </button> : null)}
        </div>
      </section>
    </div>
  );
}

function AppDetail({ target, appId, onBack, onDeleted }: { target: string; appId: string; onBack: () => void; onDeleted: (result: AppDeletePayload) => void }) {
  const t = useT();
  const language = useLanguage();
  const [tab, setTab] = useState<Tab>("deployments");
  const [action, setAction] = useState<AppAction | null>(null);
  const [message, setMessage] = useState<Message | null>(null);
  const load = useCallback(() => bridge.appGet(target, appId), [target, appId]);
  const query = useManagementQuery(load);
  const operation = useManagementOperation();
  const spec = query.data?.app?.spec;
  const active = query.data?.active_release_id;
  const execute = async (ref: string, confirmation: string) => {
    if (!action || !query.data || !spec) return;
    setMessage(null);
    const performed = action;
    if (performed === "deploy") {
      const result = await operation.run(async () => {
        const value = await bridge.appDeploy(target, appId, ref);
        if (isCancelled(value)) return value;
        if (!value.release_id || !value.image_id) throw new Error(t("operation.no_success"));
        return value;
      });
      if (!result) return;
      setMessage({ key: "operation.deployed", params: { release: result.release_id }, output: result.output, truncated: result.truncated });
    } else if (performed === "rollback") {
      const result = await operation.run(() => bridge.appRollback(target, appId, confirmation));
      if (!result) return;
      setMessage({ key: "operation.rolled_back", params: { from: result.from_release_id, to: result.to_release_id }, detail: result.recreated ? t("operation.recreated") : undefined });
    } else {
      if (!canDeleteApp(query.data)) return;
      const result = await operation.run(() => bridge.appDelete(target, appId, confirmation));
      if (!result) return;
      onDeleted(result);
      return;
    }
    setAction(null);
    await query.reload();
  };
  const update = async (draft: AppDraft) => {
    if (!spec) return;
    setMessage(null);
    const result = await operation.run(() => bridge.appUpdate(target, appId, editableUpdate(draft, spec)));
    if (!result) return;
    setMessage({ key: "operation.saved", detail: [result.proxy_detail, result.env_detail, result.volume_detail].filter(Boolean).join("\n") });
    await query.reload();
  };
  return (
    <div className="screen" data-testid="screen-app-detail">
      <button type="button" className="btn btn-ghost back-button" disabled={operation.pending} onClick={onBack} data-testid="apps-back">{t("apps.back")}</button>
      <header className="stage-head resource-head"><div><p className="eyebrow">{t("apps.application")}</p><h1 className="title mono">{appId}</h1><p className="meta mono">{spec ? `${spec.git_host}/${spec.git_owner}/${spec.git_repo}` : target}</p></div><div className="panel-actions"><button type="button" className="btn btn-primary" disabled={!spec || operation.pending} onClick={() => { operation.clearError(); setAction("deploy"); }} data-testid="app-deploy">{t("action.deploy")}</button><button type="button" className="btn btn-ghost" disabled={!spec || !active || operation.pending} onClick={() => { operation.clearError(); setAction("rollback"); }} data-testid="app-rollback">{t("action.rollback")}</button></div></header>
      {operation.notice ? <p className="scope-note" role="status" data-testid="operation-notice">{operation.notice}</p> : null}
      {query.loading ? <p className="meta" role="status">{t("resource.loading")}</p> : null}
      {query.error ? <section className="panel glass"><p className="operation-message is-bad" role="alert" data-testid="app-detail-error">{query.error}</p><button type="button" className="btn btn-ghost" onClick={() => void query.reload()}>{t("action.retry")}</button></section> : null}
      {message ? <section className="panel glass" role="status" data-testid="operation-result"><p className="is-ok">{t(message.key, message.params)}</p>{message.detail ? <p className="operation-message">{message.detail}</p> : null}{message.output ? <details><summary>{t("operation.build_output")}</summary><pre className="log-output mono">{message.output}</pre></details> : null}{message.truncated ? <p className="scope-note">{t("logs.truncated")}</p> : null}</section> : null}
      {spec && query.data ? <>
        <section className="panel glass app-overview"><div><span className="label">{t("apps.active")}</span><p className="mono" data-testid="app-active-release">{active === undefined ? t("apps.active_unknown") : active || t("apps.not_deployed")}</p></div><div><span className="label">{t("form.domain")}</span><p className="mono">{spec.domain || t("apps.no_domain")}</p></div><div><span className="label">{t("form.replicas")}</span><p>{spec.replicas}</p></div></section>
        <nav className="detail-tabs" aria-label={t("apps.tabs")}>
          {(["deployments", "logs", "configuration"] as const).map((name) => <button type="button" className={tab === name ? "detail-tab is-active" : "detail-tab"} key={name} aria-current={tab === name ? "page" : undefined} onClick={() => setTab(name)} disabled={operation.pending} data-testid={`app-tab-${name}`}>{t(`apps.tab_${name}`)}</button>)}
        </nav>
        {tab === "deployments" ? <section className="panel glass"><div className="panel-head"><h2 className="section-title">{t("apps.history")}</h2><button type="button" className="btn btn-ghost" disabled={operation.pending} onClick={() => void query.reload()} data-testid="app-detail-refresh">{t("status.refresh")}</button></div><p className="scope-note">{t("apps.build_status_note")}</p><div className="release-list">
          {query.data.releases.length === 0 ? <p className="meta">{t("apps.no_releases")}</p> : null}
          {query.data.releases.map((release) => <details className="release-row" key={release.release_id} data-testid={`release-${release.release_id}`}><summary><span className="mono">{release.release_id}</span><span className="release-commit mono">{release.commit_sha.slice(0, 12)}</span><span className={`badge is-${release.status === "RELEASE_STATUS_FAILED" ? "bad" : release.status === "RELEASE_STATUS_BUILT" ? "ok" : "warn"}`}>{t(`release.${release.status}`)}</span>{active === release.release_id ? <span className="pill is-ok">{t("apps.live")}</span> : null}</summary><dl className="rows"><div className="row"><dt>{t("release.commit")}</dt><dd className="mono">{release.commit_sha}</dd></div><div className="row"><dt>{t("release.image")}</dt><dd className="mono">{release.image_id || t("release.no_image")}</dd></div><div className="row"><dt>{t("release.started")}</dt><dd>{formatTime(release.started_at, language)}</dd></div><div className="row"><dt>{t("release.finished")}</dt><dd>{formatTime(release.finished_at, language)}</dd></div><div className="row"><dt>{t("release.detail")}</dt><dd className="operation-message">{release.detail || "—"}</dd></div></dl></details>)}
        </div></section> : tab === "logs" ? <AppLogs target={target} appId={appId} /> : <section className="panel glass"><h2 className="section-title">{t("apps.tab_configuration")}</h2><AppForm original={spec} pending={operation.pending} error={operation.error} onSave={(draft) => void update(draft)} /><div className="danger-zone"><h3>{t("apps.delete_title")}</h3><p className="scope-note">{active === undefined ? t("apps.delete_unknown") : active ? t("apps.delete_live") : t("apps.delete_scope")}</p><button type="button" className="btn btn-danger" disabled={!canDeleteApp(query.data) || operation.pending} onClick={() => { operation.clearError(); setAction("delete"); }} data-testid="app-delete">{t("action.delete")}</button></div></section>}
      </> : null}
      {action && spec ? <OperationDialog action={action} appId={appId} branch={spec.git_branch} pending={operation.pending} error={operation.error} notice={operation.notice} onConfirm={(ref, confirmation) => void execute(ref, confirmation)} onClose={() => setAction(null)} /> : null}
    </div>
  );
}

function AppLogs({ target, appId }: { target: string; appId: string }) {
  const t = useT();
  const [tail, setTail] = useState("100");
  const load = useCallback(() => bridge.appLogs(target, appId, Number(tail)), [target, appId, tail]);
  const query = useManagementQuery(load);
  return <section className="panel glass"><div className="panel-head"><h2 className="section-title">{t("apps.tab_logs")}</h2><div className="panel-actions"><label className="filter">{t("logs.tail")}<select className="sel" value={tail} onChange={(event) => setTail(event.target.value)} data-testid="logs-tail">{["100", "500", "1000"].map((value) => <option value={value} key={value}>{value}</option>)}</select></label><button type="button" className="btn btn-ghost" disabled={query.loading} onClick={() => void query.reload()} data-testid="logs-refresh">{t("status.refresh")}</button></div></div><p className="scope-note">{t("logs.snapshot")}</p>{query.error ? <p className="operation-message is-bad" role="alert">{query.error}</p> : null}{query.loading ? <p className="meta" role="status">{t("resource.loading")}</p> : <pre className="log-output mono" data-testid="app-logs">{query.data?.output || t("logs.empty")}</pre>}{query.data?.truncated ? <p className="scope-note">{t("logs.truncated")}</p> : null}</section>;
}

import { useEffect, useRef, useState } from "react";

import { bridge } from "../bridge.ts";
import { EmptyState } from "../components/EmptyState.tsx";
import { GitHubDisconnectDialog } from "../components/GitHubDisconnectDialog.tsx";
import { KeyValues } from "../components/KeyValues.tsx";
import { Tile } from "../components/Tile.tsx";
import { useGitHub } from "../hooks/useGitHub.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { errorText } from "../lib/errors.ts";
import { formatRemaining, inlineError, pendingExpired, rejectedNotice, remainingSeconds, type GitHubConnection, type GitHubInstallation } from "../lib/github.ts";
import { useToast } from "../toast/ToastProvider.tsx";

const COPIED_MS = 2000;
const TICK_MS = 1000;
const MANAGE_INSTALLATIONS = "https://github.com/settings/installations";
const MANAGE_AUTHORIZATIONS = "https://github.com/settings/apps/authorizations";

export function GitHubScreen({ target }: { target: string | null }) {
  const t = useT();
  if (!target) return <EmptyState title={t("sub.github")} body={t("apps.connect_first")} />;
  return <GitHub target={target} />;
}

/** Kalan süre; pencere gizliyken ya da süre bilinmiyorken saymaz. */
function useRemaining(expiresAt: number | null): number | null {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (expiresAt === null) return;
    const tick = () => { if (!document.hidden) setNow(Date.now()); };
    const timer = window.setInterval(tick, TICK_MS);
    document.addEventListener("visibilitychange", tick);
    tick();
    return () => { window.clearInterval(timer); document.removeEventListener("visibilitychange", tick); };
  }, [expiresAt]);
  return remainingSeconds(expiresAt, now);
}

function useOpenGitHub(): (url: string) => void {
  const toast = useToast();
  return (url) => { bridge.openGitHub(url).catch((err: unknown) => toast("toast.github_open_failed", { error: errorText(err) })); };
}

function GitHub({ target }: { target: string }) {
  const t = useT();
  const gh = useGitHub(target);
  const { view } = gh;
  const [confirming, setConfirming] = useState(false);
  const refresh = <button type="button" className="btn btn-ghost" disabled={!view.loaded || gh.busy} onClick={gh.reload} data-testid="github-refresh">{t("status.refresh")}</button>;
  const head = <header className="stage-head resource-head"><div><h1 className="title">{t("sub.github")}</h1><p className="meta">{t("github.subtitle")}</p></div>{view.unsupported ? null : refresh}</header>;
  const c = view.connection;
  let body;
  if (!view.loaded) body = <p className="meta" role="status">{t("resource.loading")}</p>;
  else if (view.unsupported) body = <EmptyState title={t("github.upgrade")} body={t("github.upgrade_body")} />;
  else if (!c) body = <section className="panel"><div className="well"><p className="operation-message is-bad" role="alert" data-testid="github-error">{view.error}</p></div></section>;
  else body = <GitHubBody connection={c} view={view} gh={gh} onDisconnect={() => setConfirming(true)} />;
  return (
    <div className="screen" data-testid="screen-github">
      {head}
      {inlineError(view.error, c !== null, confirming) ? <p className="operation-message is-bad" role="alert" data-testid="github-error">{view.error}</p> : null}
      {gh.notice ? <p className="scope-note" role="status" data-testid="operation-notice">{gh.notice}</p> : null}
      {body}
      {confirming ? <GitHubDisconnectDialog pending={gh.busy} error={view.error} onClose={() => setConfirming(false)} onConfirm={() => void gh.disconnect().then((r) => { if (r !== "failed") setConfirming(false); })} /> : null}
    </div>
  );
}

interface BodyProps {
  connection: GitHubConnection;
  view: ReturnType<typeof useGitHub>["view"];
  gh: ReturnType<typeof useGitHub>;
  onDisconnect: () => void;
}

function GitHubBody({ connection: c, view, gh, onDisconnect }: BodyProps) {
  const t = useT();
  if (c.state === "pending") return <Pending connection={c} busy={gh.busy} onConnect={() => void gh.connect()} />;
  if (c.state === "connected") return <Connected connection={c} busy={gh.busy} onDisconnect={onDisconnect} onReconnect={() => void gh.connect()} />;
  const message = c.state === "failed" && c.failureKey ? t(c.failureKey) : view.ended ? t("github.ended") : t("github.not_connected");
  return (
    <section className="panel" data-testid={`github-${c.state}`}>
      <div className="well">
        {rejectedNotice(c) ? <p className="operation-message is-warn" role="alert" data-testid="github-reconnect-rejected">{t("github.reconnect_rejected")}</p> : null}
        <p className={c.state === "failed" ? "operation-message is-bad" : "meta"} role={c.state === "failed" ? "alert" : "status"} data-testid="github-message">{message}</p>
        <div className="form-actions"><button type="button" className="btn btn-primary" disabled={gh.busy} onClick={() => void gh.connect()} data-testid="github-connect">{gh.busy ? t("github.connecting") : t("github.connect")}</button></div>
      </div>
    </section>
  );
}

function Pending({ connection: c, busy, onConnect }: { connection: GitHubConnection; busy: boolean; onConnect: () => void }) {
  const t = useT();
  const toast = useToast();
  const open = useOpenGitHub();
  const prompt = c.prompt;
  const left = useRemaining(prompt?.expiresAt ?? null);
  const expired = pendingExpired(left);
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => () => { if (timer.current) clearTimeout(timer.current); }, []);
  const code = prompt?.userCode ?? null;
  const copy = async () => {
    if (!code) return;
    try {
      await bridge.copyText(code);
      setCopied(true);
      if (timer.current) clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), COPIED_MS);
    } catch (err) {
      toast("toast.copy_failed", { error: errorText(err) });
    }
  };
  return (
    <section className="panel" data-testid="github-pending">
      <div className="well gh-pending">
        {rejectedNotice(c) ? <p className="operation-message is-warn" role="alert" data-testid="github-reconnect-rejected">{t("github.reconnect_rejected")}</p> : null}
        {code ? <div className="gh-code"><Tile label={t("github.code_label")} value={code} testId="github-code" /></div> : <p className="operation-message is-warn" role="alert" data-testid="github-code-hidden">{t("github.code_hidden")}</p>}
        <div className="form-actions">
          {code ? <button type="button" className="btn btn-primary" onClick={() => void copy()} data-testid="github-copy">{copied ? t("github.copied") : t("github.copy")}</button> : null}
          {prompt?.verificationUri ? <button type="button" className="btn btn-ghost" onClick={() => open(prompt.verificationUri ?? "")} data-testid="github-open-device">{t("github.open_device")}</button> : null}
          {prompt?.installUrl ? <button type="button" className="btn btn-ghost" onClick={() => open(prompt.installUrl ?? "")} data-testid="github-open-install">{t("github.open_install")}</button> : null}
        </div>
        {expired ? null : <p className="meta" role="status" data-testid="github-waiting">{t("github.waiting")}</p>}
        {left !== null ? <p className="hint" data-testid="github-remaining">{expired ? t("github.expired") : t("github.expires_in", { time: formatRemaining(left) })}</p> : null}
        {expired ? <div className="form-actions"><button type="button" className="btn btn-primary" disabled={busy} onClick={onConnect} data-testid="github-connect">{busy ? t("github.connecting") : t("github.connect")}</button></div> : null}
      </div>
    </section>
  );
}

function Connected({ connection: c, busy, onDisconnect, onReconnect }: { connection: GitHubConnection; busy: boolean; onDisconnect: () => void; onReconnect: () => void }) {
  const t = useT();
  const open = useOpenGitHub();
  return (
    <section className="panel" data-testid="github-connected">
      <div className="well gh-connected">
        <KeyValues testId="github-account" rows={[{ label: t("github.account"), value: c.login, mono: true }, { label: t("github.installations"), value: String(c.installations.length) }]} />
        {c.reconnectRequired ? <div><p className="operation-message is-warn" role="alert" data-testid="github-reconnect">{t("github.reconnect_required")}</p><div className="form-actions"><button type="button" className="btn btn-primary" disabled={busy} onClick={onReconnect} data-testid="github-reconnect-button">{t("github.reconnect")}</button></div></div> : null}
        {c.hasAllRepositories ? <p className="scope-banner is-warn" role="alert" data-testid="github-all-warning">{t("github.all_warning")}</p> : null}
        {c.installations.length === 0 ? <p className="meta" data-testid="github-no-installation">{t("github.no_installation")}</p> : null}
        <div className="gh-installations">{c.installations.map((i) => <Installation key={i.account} installation={i} />)}</div>
        <div className="form-actions">
          {c.installUrl ? <button type="button" className="btn btn-ghost" onClick={() => open(c.installUrl ?? "")} data-testid="github-open-install">{t("github.open_install")}</button> : null}
          <button type="button" className="btn btn-ghost" onClick={() => open(MANAGE_INSTALLATIONS)}>{t("github.manage_installations")}</button>
          <button type="button" className="btn btn-ghost" onClick={() => open(MANAGE_AUTHORIZATIONS)}>{t("github.manage_authorizations")}</button>
        </div>
        <div className="danger-zone">
          <h3>{t("github.disconnect_title")}</h3>
          <p className="scope-note">{t("github.disconnect_scope")}</p>
          <button type="button" className="btn btn-danger" disabled={busy} onClick={onDisconnect} data-testid="github-disconnect">{t("github.disconnect")}</button>
        </div>
      </div>
    </section>
  );
}

function Installation({ installation: i }: { installation: GitHubInstallation }) {
  const t = useT();
  const count = i.repositories.length;
  const scope = i.allRepositories ? t("github.all_repositories") : i.truncated ? t("github.selected_truncated", { count }) : t("github.selected_count", { count });
  return (
    <article className="gh-installation" data-testid="github-installation">
      <div className="alarm-top"><h2 className="mono">{i.account}</h2><span className={i.allRepositories ? "badge is-warn" : "badge"}>{scope}</span></div>
      <ul className="gh-repos">{i.repositories.map((r) => <li key={r.fullName} className="gh-repo" data-testid="github-repo"><span className="mono">{r.fullName}</span>{r.isPrivate ? <span className="badge" data-testid="github-private">{t("github.private")}</span> : null}</li>)}</ul>
    </article>
  );
}

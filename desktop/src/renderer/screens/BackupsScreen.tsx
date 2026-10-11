import { useCallback, useState } from "react";

import { bridge } from "../bridge.ts";
import { EmptyState } from "../components/EmptyState.tsx";
import { useManagementOperation } from "../hooks/useManagementOperation.ts";
import { useManagementQuery } from "../hooks/useManagementQuery.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { formatTime, humanBytes } from "../lib/format.ts";
import type { BackupCreatePayload } from "../lib/types.ts";

export function BackupsScreen({ target }: { target: string | null }) {
  const t = useT();
  if (!target) return <EmptyState title={t("backups.title")} body={t("apps.connect_first")} />;
  return <BackupList target={target} />;
}

function BackupList({ target }: { target: string }) {
  const t = useT();
  const language = useLanguage();
  const load = useCallback(() => bridge.backupsList(target), [target]);
  const query = useManagementQuery(load);
  const operation = useManagementOperation();
  const [created, setCreated] = useState<BackupCreatePayload | null>(null);
  const create = async () => {
    setCreated(null);
    const result = await operation.run(() => bridge.backupCreate(target));
    if (!result) return;
    setCreated(result);
    await query.reload();
  };
  return <div className="screen" data-testid="screen-backups"><header className="stage-head resource-head"><div><p className="eyebrow">{t("backups.eyebrow")}</p><h1 className="title">{t("backups.title")}</h1><p className="meta">{t("backups.subtitle")}</p></div><button type="button" className="btn btn-primary" disabled={operation.pending} onClick={() => void create()} data-testid="backup-create">{operation.pending ? t("operation.running") : t("backups.create")}</button></header>
    <p className="scope-banner glass" data-testid="backups-scope">{t("backups.volumes_excluded")}</p>
    {operation.error ? <p className="operation-message is-bad" role="alert" data-testid="backup-error">{operation.error}</p> : null}
    {created?.backup ? <section className="panel glass" role="status" data-testid="backup-result"><p className="is-ok">{t("backups.created")}</p><p className="mono operation-message">{created.backup.path}</p>{created.volumes_excluded ? <p className="scope-note">{t("backups.volumes_excluded")}</p> : null}</section> : null}
    <section className="panel glass"><div className="panel-head"><h2 className="section-title">{t("backups.saved")}</h2><button type="button" className="btn btn-ghost" disabled={query.loading || operation.pending} onClick={() => void query.reload()}>{t("status.refresh")}</button></div>
      {query.loading ? <p className="meta" role="status">{t("resource.loading")}</p> : null}{query.error ? <p className="operation-message is-bad" role="alert">{query.error}</p> : null}{query.data ? <p className="scope-note">{t("backups.retention", { count: query.data.keep })}</p> : null}{query.data?.backups.length === 0 ? <p className="meta">{t("backups.empty")}</p> : null}
      <div className="backup-list">{query.data?.backups.map((backup) => <article className="backup-row" key={backup.path} data-testid="backup-row"><p className="mono operation-message">{backup.path}</p><div className="resource-card-foot"><span>{formatTime(new Date(Number(backup.taken_unix) * 1000).toString(), language)}</span><span>{humanBytes(backup.bytes)}</span></div></article>)}</div>
    </section><p className="scope-note">{t("backups.restore_help")}</p></div>;
}

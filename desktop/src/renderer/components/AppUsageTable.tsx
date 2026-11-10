import { useT } from "../i18n/I18nProvider.tsx";
import { useResourceText } from "../hooks/useResourceText.ts";
import { humanBytes } from "../lib/format.ts";
import { sortApps, type AppCounters, type ResourceRates } from "../lib/resources.ts";

interface Props {
  /** null: ölçülemedi (executor yanıt vermedi); boş dizi: çalışan uygulama yok. */
  apps: readonly AppCounters[] | null;
  rates: ResourceRates | null;
}

/**
 * Uygulama başına kaynak (K-166): en çok CPU kullanan üstte. Ağ ve disk iki
 * satırlı hücrede (gelen/giden, okuma/yazma) ki dar pencerede tablo taşmasın;
 * görev ve konteyner sütunları dar pencerede gizlenir.
 */
export function AppUsageTable({ apps, rates }: Props) {
  const t = useT();
  if (apps === null || apps.length === 0) {
    return (
      <p className="audit-placeholder" data-testid="app-usage-placeholder">
        {apps === null ? t("res.apps_unmeasured") : t("res.apps_none")}
      </p>
    );
  }
  const r: ResourceRates = rates ?? { cpu: null, iowait: null, steal: null, netRx: null, netTx: null, diskRead: null, diskWrite: null, apps: new Map() };
  return (
    <div className="table-wrap">
      <table className="data-table res-table" data-testid="app-usage">
        <thead>
          <tr>
            <th>{t("res.col_app")}</th>
            <th className="num">{t("res.cpu")}</th>
            <th className="num">{t("res.memory")}</th>
            <th className="num">{t("res.col_network")}</th>
            <th className="num">{t("res.col_disk")}</th>
            <th className="num res-wide">{t("res.tasks")}</th>
            <th className="num res-wide">{t("res.col_containers")}</th>
          </tr>
        </thead>
        <tbody>
          {sortApps(apps, r).map((app) => (
            <AppRow key={app.appId} app={app} rates={r} />
          ))}
        </tbody>
      </table>
    </div>
  );
}

function AppRow({ app, rates }: { app: AppCounters; rates: ResourceRates }) {
  const t = useT();
  const text = useResourceText();
  const r = rates.apps.get(app.appId);
  return (
    <tr data-testid="app-usage-row" data-app={app.appId}>
      <td className="cell-fit cell-strong" title={app.appId}>{app.appId}</td>
      <td className="num mono" data-testid="app-cpu">{text.percent(r?.cpu ?? null)}</td>
      <td className="num mono" title={app.memoryLimit > 0 ? t("res.memory_limit", { limit: humanBytes(app.memoryLimit) }) : undefined}>{humanBytes(app.memory)}</td>
      <td className="num mono">
        <span className="res-pair">
          <span title={t("res.in")}>{text.rate(r?.netRx ?? null)}</span>
          <span className="cell-faint" title={t("res.out")}>{text.rate(r?.netTx ?? null)}</span>
        </span>
      </td>
      <td className="num mono">
        <span className="res-pair">
          <span title={t("res.read")}>{text.rate(r?.diskRead ?? null)}</span>
          <span className="cell-faint" title={t("res.write")}>{text.rate(r?.diskWrite ?? null)}</span>
        </span>
      </td>
      <td className="num mono res-wide">{app.tasks}</td>
      <td className="num mono res-wide">{app.containers}</td>
    </tr>
  );
}

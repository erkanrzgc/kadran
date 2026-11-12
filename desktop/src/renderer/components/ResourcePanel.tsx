import { useLanguage, useT, type Translate } from "../i18n/I18nProvider.tsx";
import { useResources, type ResourceView } from "../hooks/useResources.ts";
import { useResourceText, type ResourceText } from "../hooks/useResourceText.ts";
import { formatCount, decimal, humanBytes, humanDuration } from "../lib/format.ts";
import { scaleMax } from "../lib/resource-chart.ts";
import { HISTORY_SAMPLES, percentUsed, type HostCounters } from "../lib/resources.ts";
import { AppUsageTable } from "./AppUsageTable.tsx";
import { KeyValues, type KeyValue } from "./KeyValues.tsx";
import { ResourceChart } from "./ResourceChart.tsx";
import { Tile } from "./Tile.tsx";

/** Sunucu sayfası açıkken ve pencere görünürken bu aralıkla yoklanır. */
const POLL_MS = 2_000;

/**
 * Sunucu sayfasının canlı kaynak bölümü (K-166): host karoları ve
 * ayrıntıları, altında uygulama tablosu. Yoklama yalnız bu bölüm ekrandayken.
 * Eski sunucu (RPC yok) hata değil, "sunucuyu yükseltin" der.
 */
export function ResourcePanel({ target }: { target: string }) {
  const t = useT();
  const view = useResources(target, POLL_MS, true);
  return (
    <>
      <section className="panel" data-testid="resources" data-state={view.state}>
        <div className="panel-head">
          <div>
            <h2 className="section-title">{t("res.title")}</h2>
            <p className="panel-sub">{t("res.sub")}</p>
          </div>
          {view.state === "live" ? (
            <span className="res-live">
              <i className="live-dot is-on" aria-hidden="true" />
              {t("res.live")}
            </span>
          ) : null}
        </div>
        <div className="well">
          <HostBody view={view} />
        </div>
      </section>
      {view.snapshot && view.state !== "unsupported" ? (
        <section className="panel">
          <div className="panel-head">
            <div>
              <h2 className="section-title">{t("res.apps")}</h2>
              <p className="panel-sub">{t("res.apps_sub")}</p>
            </div>
          </div>
          <div className="well is-flush">
            <AppUsageTable apps={view.snapshot.apps} rates={view.rates} />
          </div>
        </section>
      ) : null}
    </>
  );
}

function HostBody({ view }: { view: ResourceView }) {
  const t = useT();
  if (view.state === "unsupported") {
    return <p className="empty" data-testid="resources-unsupported">{t("res.unsupported")}</p>;
  }
  if (!view.snapshot) {
    return view.state === "failed" ? <p className="empty is-warn" role="status">{t("res.failed")}</p> : <ResourceTiles view={view} />;
  }
  return (
    <>
      {view.state === "failed" ? <p className="res-note is-warn" role="status">{t("res.failed_stale")}</p> : null}
      <ResourceTiles view={view} />
      <HostDetails host={view.snapshot.host} />
    </>
  );
}

function ResourceTiles({ view }: { view: ResourceView }) {
  const t = useT();
  const text = useResourceText();
  const host = view.snapshot?.host ?? null;
  const r = view.rates;
  const h = view.history;
  const netMax = scaleMax([...h.netRx, ...h.netTx]);
  const diskMax = scaleMax([...h.diskRead, ...h.diskWrite]);
  return (
    <div className="res-tiles" data-testid="resource-tiles">
      <Tile label={t("res.cpu")} value={text.percent(r?.cpu ?? null)} testId="res-cpu">
        <span className="res-sub mono">{t("res.cpu_detail", { iowait: text.percent(r?.iowait ?? null), steal: text.percent(r?.steal ?? null) })}</span>
        <ResourceChart slots={HISTORY_SAMPLES} series={[{ key: "cpu", label: t("res.cpu"), values: h.cpu, tone: 1 }]} max={100} format={text.percent} label={t("res.cpu")} />
      </Tile>
      <Tile label={t("res.memory")} value={usedOf(host?.memoryTotal ?? null, host?.memoryAvailable ?? null)} testId="res-memory">
        <span className="res-sub mono">{swapText(host, t)}</span>
        <ResourceChart slots={HISTORY_SAMPLES} series={[{ key: "memory", label: t("res.memory"), values: h.memory, tone: 1 }]} max={100} format={text.percent} label={t("res.memory")} />
      </Tile>
      <Tile label={t("res.network")} value={t("res.in_rate", { rate: text.rate(r?.netRx ?? null) })} testId="res-network">
        <span className="res-sub mono">{t("res.out_rate", { rate: text.rate(r?.netTx ?? null) })}</span>
        <ResourceChart slots={HISTORY_SAMPLES} series={pair(t("res.in"), h.netRx, t("res.out"), h.netTx)} max={netMax} format={text.rate} label={t("res.network")} />
      </Tile>
      <Tile label={t("res.disk_io")} value={t("res.read_rate", { rate: text.rate(r?.diskRead ?? null) })} testId="res-disk">
        <span className="res-sub mono">{t("res.write_rate", { rate: text.rate(r?.diskWrite ?? null) })}</span>
        <ResourceChart slots={HISTORY_SAMPLES} series={pair(t("res.read"), h.diskRead, t("res.write"), h.diskWrite)} max={diskMax} format={text.rate} label={t("res.disk_io")} />
      </Tile>
    </div>
  );
}

function pair(first: string, a: readonly (number | null)[], second: string, b: readonly (number | null)[]) {
  return [
    { key: "a", label: first, values: a, tone: 1 as const },
    { key: "b", label: second, values: b, tone: 2 as const },
  ];
}

/** "kullanılan / toplam"; biri bilinmiyorsa "—". */
function usedOf(total: number | null, available: number | null): string {
  if (total === null || available === null || total <= 0) return "—";
  return `${humanBytes(Math.max(0, total - available))} / ${humanBytes(total)}`;
}

function swapText(host: HostCounters | null, t: Translate): string {
  if (!host || host.swapTotal === null || host.swapFree === null) return "—";
  if (host.swapTotal === 0) return t("res.no_swap");
  return t("res.swap", { used: humanBytes(Math.max(0, host.swapTotal - host.swapFree)), total: humanBytes(host.swapTotal) });
}

function HostDetails({ host }: { host: HostCounters }) {
  const t = useT();
  const language = useLanguage();
  const text = useResourceText();
  const rows: KeyValue[] = [
    { label: t("res.disk_space"), value: diskText(host, text), mono: true },
    { label: t("res.inodes"), value: text.percent(percentUsed(host.inodesTotal, host.inodesFree)), mono: true },
    { label: t("res.load"), value: loadText(host, language, t), mono: true },
    { label: t("res.pressure"), value: [host.cpuPressure, host.memoryPressure, host.ioPressure].map(text.percent).join(" · "), mono: true },
    { label: t("res.tasks"), value: host.tasks === null ? "—" : formatCount(host.tasks, language), mono: true },
    { label: t("res.host_uptime"), value: host.uptime === null ? "—" : humanDuration(host.uptime), mono: true },
  ];
  return (
    <div className="res-details">
      <KeyValues rows={rows} testId="resource-details" />
    </div>
  );
}

function diskText(host: HostCounters, text: ResourceText): string {
  const used = usedOf(host.diskTotal, host.diskAvailable);
  return used === "—" ? used : `${used} · ${text.percent(percentUsed(host.diskTotal, host.diskAvailable))}`;
}

function loadText(host: HostCounters, language: string, t: Translate): string {
  if (host.load1 === null || host.load5 === null || host.load15 === null) return "—";
  const f = (v: number) => decimal(v, 2, language);
  // Çekirdek sayısı bilinmiyorsa yalnız yükler: "0 çekirdek" yanlış olurdu.
  if (host.cpuCount <= 0) return [host.load1, host.load5, host.load15].map(f).join(" · ");
  return t("res.load_value", { l1: f(host.load1), l5: f(host.load5), l15: f(host.load15), count: host.cpuCount });
}

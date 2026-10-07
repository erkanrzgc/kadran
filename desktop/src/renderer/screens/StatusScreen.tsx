import { useCallback, useEffect, useRef, useState } from "react";

import { AuditTable } from "../components/AuditTable.tsx";
import { ChainCard } from "../components/ChainCard.tsx";
import { EmptyState } from "../components/EmptyState.tsx";
import { InfoCard } from "../components/InfoCard.tsx";
import { Sparkline } from "../components/Sparkline.tsx";
import { Tile } from "../components/Tile.tsx";
import { useAudit } from "../hooks/useAudit.ts";
import type { ServerApi } from "../hooks/useServer.ts";
import { useT } from "../i18n/I18nProvider.tsx";
import { chainView, worstTone } from "../lib/chain.ts";
import { humanDuration, memoryText } from "../lib/format.ts";

/** Coolify'daki gibi: açıkken her 10 saniyede yenile. Varsayılan kapalı. */
const LIVE_INTERVAL_MS = 10_000;
const PREVIEW_SIZE = 10;

export function StatusScreen({ server, onSeeAll }: { server: ServerApi; onSeeAll: () => void }) {
  const t = useT();
  const { target, status, verify, rtt, memorySamples, rttSamples } = server.state;
  const [live, setLive] = useState(false);
  const audit = useAudit(target, PREVIEW_SIZE);
  const { refresh } = server;
  const { loadNewest } = audit;
  // Tur belirteci: hedef değişince sıfırlanır, eski turun finally'si yenisini silmez.
  const busy = useRef<object | null>(null);
  const [refreshing, setRefreshing] = useState(false);

  // Durum ve son kayıtlar birlikte yenilenir; durum başarısızsa tek bildirim
  // yeter, kayıtlar istenmez. Önceki tur bitmediyse yenisi başlamaz.
  const reload = useCallback(async () => {
    if (busy.current) return;
    const round = {};
    busy.current = round;
    setRefreshing(true);
    try {
      if (await refresh()) await loadNewest();
    } finally {
      if (busy.current === round) {
        busy.current = null;
        setRefreshing(false);
      }
    }
  }, [refresh, loadNewest]);

  // Hedef değişince önceki sunucunun yavaş turu yeni sunucunun yenilemesini engellemesin.
  useEffect(() => {
    busy.current = null;
    setRefreshing(false);
  }, [target]);

  // Aralık; kapatınca, hedef değişince ve ekran kapanınca temizlenir.
  useEffect(() => {
    if (!live || !target) return;
    const id = setInterval(() => void reload(), LIVE_INTERVAL_MS);
    return () => clearInterval(id);
  }, [live, target, reload]);

  if (!target || !status) return <EmptyState title={t("status.empty_title")} body={t("status.empty_body")} />;

  const host = status.host;
  const reachable = status.executor_reachable;
  const root = status.running_as_user === "root";
  const daemonChain = chainView(verify?.daemon_status);
  const execChain = chainView(verify?.executor_status);

  return (
    <div className="screen" data-testid="screen-status">
      <header className="stage-head">
        <p className="eyebrow">{t("status.eyebrow")}</p>
        <h1 className="title mono">{target}</h1>
        <p className="meta">{reachable ? t("status.meta_ok") : t("status.meta_down")}</p>
      </header>

      <section className="panel glass">
        <div className="panel-head">
          <h2 className="section-title">{t("status.overview")}</h2>
          <div className="panel-actions">
            <label className="live">
              <input type="checkbox" checked={live} onChange={(e) => setLive(e.target.checked)} data-testid="live" />
              <i className={live ? "live-dot is-on" : "live-dot"} aria-hidden="true" />
              {t("status.live")}
            </label>
            <button type="button" className="btn btn-ghost" disabled={refreshing} onClick={() => void reload()}>
              {t("status.refresh")}
            </button>
          </div>
        </div>
        <div className="tiles">
          <Tile label={t("tile.uptime")} value={humanDuration(status.daemon_uptime_seconds)} />
          <Tile label={t("tile.memory")} value={memoryText(host)}>
            <Sparkline samples={memorySamples} label={t("tile.memory")} />
          </Tile>
          <Tile label={t("tile.rtt")} value={rtt === null ? "—" : t("topbar.rtt", { ms: rtt })}>
            <Sparkline samples={rttSamples} label={t("tile.rtt")} />
          </Tile>
          <Tile
            label={t("tile.executor")}
            value={reachable ? t("value.reachable") : t("value.unreachable")}
            tone={reachable ? "ok" : "warn"}
          />
          <Tile
            label={t("tile.chains")}
            value={`${daemonChain.label} · ${execChain.label}`}
            tone={worstTone(daemonChain.tone, execChain.tone)}
            testId="tile-chains"
          />
        </div>
      </section>

      <section className="cards">
        <InfoCard
          title={t("card.daemon")}
          rows={[
            { label: t("row.version"), value: status.daemon_version || "—" },
            { label: t("row.uptime"), value: humanDuration(status.daemon_uptime_seconds) },
            root
              ? { label: t("row.user"), value: t("value.broken_install", { user: "root" }), tone: "bad" }
              : { label: t("row.user"), value: status.running_as_user || "—" },
          ]}
        />
        <InfoCard
          title={t("card.executor")}
          rows={[
            {
              label: t("row.state"),
              value: reachable ? t("value.reachable") : t("value.unreachable"),
              tone: reachable ? "ok" : "warn",
            },
            { label: t("row.version"), value: status.executor_version || "—" },
            { label: t("row.docker"), value: host?.docker_version || t("value.none") },
          ]}
        />
        <InfoCard
          title={t("card.host")}
          rows={[
            { label: t("row.name"), value: status.hostname || "—" },
            { label: t("row.kernel"), value: host?.kernel_version || "—" },
            { label: t("row.memory"), value: memoryText(host) },
          ]}
        />
      </section>

      <section className="chains">
        <ChainCard
          name={t("chains.daemon")}
          status={verify?.daemon_status}
          checked={verify?.records_checked}
          detail={verify?.detail}
          testId="chain-daemon"
        />
        <ChainCard
          name={t("chains.executor")}
          status={verify?.executor_status}
          checked={verify?.executor_records_checked}
          detail={verify?.executor_detail}
          testId="chain-executor"
        />
      </section>
      <p className="chain-note">{t("chains.note")}</p>

      <section className="panel glass">
        <div className="panel-head">
          <h2 className="section-title">{t("audit.recent")}</h2>
          <button type="button" className="btn btn-ghost" onClick={onSeeAll} data-testid="see-all">
            {t("audit.see_all")}
          </button>
        </div>
        <AuditTable
          records={audit.records}
          testId="audit-preview"
          placeholder={audit.records.length > 0 ? undefined : audit.loaded ? t("audit.empty") : t("audit.loading")}
        />
      </section>
    </div>
  );
}

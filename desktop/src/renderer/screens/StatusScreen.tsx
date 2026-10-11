import { useCallback, useEffect, useRef, useState } from "react";

import { AuditTable } from "../components/AuditTable.tsx";
import { ChainCard } from "../components/ChainCard.tsx";
import { SetupHelp } from "../components/SetupHelp.tsx";
import { InfoCard } from "../components/InfoCard.tsx";
import { Sparkline } from "../components/Sparkline.tsx";
import { Tile } from "../components/Tile.tsx";
import { useAudit } from "../hooks/useAudit.ts";
import type { ServerApi } from "../hooks/useServer.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { auditPlaceholderKey } from "../lib/audit-view.ts";
import { chainView, worstTone } from "../lib/chain.ts";
import { formatTime, humanDuration, memoryText } from "../lib/format.ts";
import { statusMetaKeys } from "../lib/health.ts";

/** Coolify'daki gibi: açıkken her 10 saniyede yenile. Varsayılan kapalı. */
const LIVE_INTERVAL_MS = 10_000;
const PREVIEW_SIZE = 10;

export function StatusScreen({ server, onSeeAll }: { server: ServerApi; onSeeAll: () => void }) {
  const t = useT();
  const language = useLanguage();
  const { target, status, verify, rtt, memorySamples, rttSamples, stale, verifiedAt, verifyStale } = server.state;
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

  if (!target || !status) return <SetupHelp />;

  const host = status.host;
  const reachable = status.executor_reachable;
  const root = status.running_as_user === "root";
  const daemonStatus = verifyStale && verify?.daemon_status === "CHAIN_STATUS_VALID" ? undefined : verify?.daemon_status;
  const execStatus = verifyStale && verify?.executor_status === "CHAIN_STATUS_VALID" ? undefined : verify?.executor_status;
  const daemonChain = chainView(daemonStatus);
  const execChain = chainView(execStatus);
  const previewKey = auditPlaceholderKey({
    total: audit.records.length,
    shown: audit.records.length,
    failed: audit.failed,
    loaded: audit.loaded,
  });

  return (
    <div className="screen" data-testid="screen-status">
      <header className="stage-head">
        <p className="eyebrow">{stale ? t("status.eyebrow_stale") : t("status.eyebrow")}</p>
        <h1 className="title mono">{target}</h1>
        {statusMetaKeys(stale, reachable).map((k) => (
          <p key={k} className="meta">
            {t(k)}
          </p>
        ))}
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
            <button type="button" className="btn btn-ghost" disabled={refreshing} onClick={() => void reload()} data-testid="refresh">
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
          status={daemonStatus}
          checked={verify?.records_checked}
          detail={verify?.detail}
          testId="chain-daemon"
        />
        <ChainCard
          name={t("chains.executor")}
          status={execStatus}
          checked={verify?.executor_records_checked}
          detail={verify?.executor_detail}
          testId="chain-executor"
        />
      </section>
      <p className={verifyStale ? "chain-note is-warn" : "chain-note"} data-testid="verification-time" role="status">{verifiedAt ? t("chains.verified_at", { time: formatTime(new Date(verifiedAt).toISOString(), language) }) : t("chains.never_verified")}{verifyStale ? ` ${t("chains.stale")}` : ""}</p>
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
          placeholder={previewKey ? t(previewKey) : undefined}
        />
      </section>
    </div>
  );
}

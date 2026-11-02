import type { Profile } from "../bridge.ts";
import { useLanguage, useT } from "../i18n/I18nProvider.tsx";
import { outcomeView } from "../lib/chain.ts";
import { appOfTarget, type FleetOperation } from "../lib/fleet.ts";
import { relativeTime } from "../lib/format.ts";
import { providerView } from "../lib/providers.ts";

interface Props {
  operations: readonly FleetOperation[];
  profiles: readonly Profile[];
  loaded: boolean;
  /** Hiçbir sunucu okunamadı: "işlem yok" denemez. */
  allFailed: boolean;
  testId: string;
  onOpen: (target: string) => void;
}

// Sunucudan gelen eylem adı nesne özelliğine denk gelmesin diye Map.
const OPERATION_KEY: ReadonlyMap<string, string> = new Map([
  ["app.deploy", "fleet.op_deploy"],
  ["app.rollback", "fleet.op_rollback"],
]);

const OUTCOME_KEY: ReadonlyMap<string, string> = new Map([
  ["AUDIT_OUTCOME_SUCCESS", "fleet.outcome_success"],
  ["AUDIT_OUTCOME_FAILURE", "fleet.outcome_failure"],
  ["AUDIT_OUTCOME_DENIED", "fleet.outcome_denied"],
]);

/** Bütün sunucuların işlemleri. Satırdaki sunucuya basınca o sunucunun sekmesi açılır. */
export function OperationsTable({ operations, profiles, loaded, allFailed, testId, onOpen }: Props) {
  const t = useT();
  const language = useLanguage();
  const now = Date.now();
  const emptyText = !loaded ? t("resource.loading") : allFailed ? t("fleet.records_unread") : t("fleet.no_operations");
  return (
    <div className="table-wrap">
      <table className="data-table" data-testid={testId}>
        <thead>
          <tr>
            <th scope="col">{t("fleet.col_app")}</th>
            <th scope="col">{t("fleet.col_server")}</th>
            <th scope="col">{t("fleet.col_operation")}</th>
            <th scope="col">{t("fleet.col_status")}</th>
            <th scope="col">{t("fleet.col_time")}</th>
          </tr>
        </thead>
        <tbody>
          {operations.length === 0 ? <tr><td colSpan={5} className="table-empty" data-testid={`${testId}-empty`}>{emptyText}</td></tr> : null}
          {operations.map(({ server, record }) => {
            const profile = profiles.find((p) => p.target === server);
            const provider = profile?.provider ? providerView(profile.provider).name : "";
            const view = outcomeView(record.outcome);
            const operationKey = OPERATION_KEY.get(record.action);
            return (
              <tr key={`${server}:${record.seq}`} data-testid={`${testId}-row`}>
                <td className="cell-strong">{appOfTarget(record.target) || "—"}</td>
                <td className="cell-fit cell-dim" title={server}><button type="button" className="row-button is-plain" onClick={() => onOpen(server)}>{profile?.name ?? server}</button>{provider ? <span className="cell-faint"> · {provider}</span> : null}</td>
                <td className="mono cell-dim">{operationKey ? t(operationKey) : record.action || "—"}</td>
                <td><span className={`pill is-${view.tone}`}><i className="pill-dot" aria-hidden="true" /><span style={{ color: "var(--fg)" }}>{t(OUTCOME_KEY.get(record.outcome) ?? "fleet.outcome_unknown")}</span></span></td>
                <td className="cell-dim">{relativeTime(record.ts, now, language)}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

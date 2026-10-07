/**
 * Sidecar'ın döndürdüğü JSON'un tipleri.
 *
 * Biçim protojson'dan (cmd/kadran/json.go): alanlar snake_case, uint64 ve
 * int64 METİN, enum'lar ad olarak, false/sıfır alanlar da yazılır, boş
 * mesaj alanı null. test/fixtures-shape.test.ts bu varsayımları gerçek
 * çıktıdan üretilen fikstürlere karşı ölçüyor.
 */

export type ChainStatusName =
  | "CHAIN_STATUS_UNSPECIFIED"
  | "CHAIN_STATUS_VALID"
  | "CHAIN_STATUS_INVALID"
  | "CHAIN_STATUS_UNREACHABLE";

export type AuditOutcomeName =
  | "AUDIT_OUTCOME_UNSPECIFIED"
  | "AUDIT_OUTCOME_SUCCESS"
  | "AUDIT_OUTCOME_FAILURE"
  | "AUDIT_OUTCOME_DENIED";

/** protojson'da 64 bit tamsayılar metin olarak gelir. */
export type U64 = string;

export interface HostInfo {
  os: string;
  kernel_version: string;
  architecture: string;
  cpu_count: number;
  memory_total_bytes: U64;
  memory_available_bytes: U64;
  docker_version: string;
  disk_total_bytes: U64;
  disk_available_bytes: U64;
}

export interface StatusPayload {
  daemon_version: string;
  hostname: string;
  daemon_uptime_seconds: U64;
  running_as_user: string;
  executor_reachable: boolean;
  executor_version: string;
  host: HostInfo | null;
}

export interface Actor {
  ssh_key_fingerprint: string;
  source_ip: string;
  label: string;
  origin: string;
}

export interface AuditRecord {
  seq: U64;
  ts: string | null;
  actor: Actor | null;
  action: string;
  target: string;
  params_json: string;
  outcome: AuditOutcomeName;
  detail: string;
  source: string;
}

export interface AuditListPayload {
  records: AuditRecord[];
  latest_seq: U64;
}

export interface VerifyPayload {
  daemon_status: ChainStatusName;
  records_checked: U64;
  first_invalid_seq: U64;
  detail: string;
  executor_status: ChainStatusName;
  executor_records_checked: U64;
  executor_detail: string;
}

export interface VersionPayload {
  version: string;
  commit: string;
  protocol: number;
}

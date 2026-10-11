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

export interface ResourceLimits { memory_bytes: U64; cpu_millis: number; blkio_weight: number }
export interface AppVolume { name: string; mount_path: string; read_only: boolean }
export interface AppSpec {
  app_id: string; git_host: string; git_owner: string; git_repo: string; git_branch: string;
  dockerfile_path: string; build_args: Record<string, string>; container_port: number;
  replicas: number; health_path: string; domain: string; limits: ResourceLimits;
  env: Record<string, string>; volumes: AppVolume[];
}
export interface AppUpdate {
  domain?: string; git_branch?: string; health_path?: string; replicas?: number;
  env?: Record<string, string>; env_remove?: string[]; volumes?: AppVolume[]; volume_remove?: string[];
}
export interface ManagedApp { spec: AppSpec; created_at: string | null; updated_at: string | null; release_count: number }
export interface AppRelease { release_id: string; app_id: string; commit_sha: string; status: string; image_id: string; started_at: string | null; finished_at: string | null; detail: string }
export interface AppsListPayload { apps: ManagedApp[] }
export interface AppGetPayload { app: ManagedApp; releases: AppRelease[]; active_release_id?: string }
export interface AppWritePayload { app: ManagedApp; proxy_detail?: string; env_detail?: string; volume_detail?: string }
export interface AppDeletePayload { app_id: string; containers_removed: number; releases_deleted: number; deployments_deleted: number; volumes_kept: string[] }
export interface RollbackPayload { app_id: string; from_release_id: string; to_release_id: string; recreated: boolean }
export interface DeployPayload { release_id: string; image_id: string; output: string; truncated: boolean }
export interface LogsPayload { output: string; truncated: boolean }
export interface AlarmInfo { id: string; kind: string; target: string; severity: string; since_unix: U64; detail: string }
export interface AlarmsPayload { alarms: AlarmInfo[]; delivery_is_local_only: boolean }
export interface BackupInfo { path: string; bytes: U64; taken_unix: U64 }
export interface BackupsPayload { backups: BackupInfo[]; keep: number }
export interface BackupCreatePayload { backup: BackupInfo; volumes_excluded: boolean }

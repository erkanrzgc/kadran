/** Validate the fields the renderer consumes, rather than treating TypeScript as a runtime check. */
type ObjectValue = Record<string, unknown>;
const object = (v: unknown): v is ObjectValue => !!v && typeof v === "object" && !Array.isArray(v);
const text = (v: unknown): v is string => typeof v === "string";
const uint = (v: unknown) => typeof v === "number" && Number.isSafeInteger(v) && v >= 0;
const u64 = (v: unknown) => text(v) && /^\d+$/.test(v);
const time = (v: unknown) => v === null || text(v);
const strings = (v: unknown) => Array.isArray(v) && v.every(text);
const stringMap = (v: unknown) => object(v) && Object.values(v).every(text);
const texts = (v: ObjectValue, fields: string[]) => fields.every((field) => text(v[field]));
const array = (v: unknown, predicate: (item: unknown) => boolean) => Array.isArray(v) && v.every(predicate);
const volume = (v: unknown) => object(v) && texts(v, ["name", "mount_path"]) && typeof v.read_only === "boolean";
function app(v: unknown): boolean {
  if (!object(v) || !object(v.spec) || !uint(v.release_count) || !time(v.created_at) || !time(v.updated_at)) return false;
  const s = v.spec;
  return texts(s, ["app_id", "git_host", "git_owner", "git_repo", "git_branch", "dockerfile_path", "health_path", "domain"])
    && uint(s.container_port) && uint(s.replicas) && object(s.limits) && u64(s.limits.memory_bytes) && uint(s.limits.cpu_millis) && uint(s.limits.blkio_weight)
    && stringMap(s.env) && stringMap(s.build_args) && array(s.volumes, volume);
}
const release = (v: unknown) => object(v) && texts(v, ["release_id", "app_id", "commit_sha", "status", "image_id", "detail"]) && time(v.started_at) && time(v.finished_at);
const alarm = (v: unknown) => object(v) && texts(v, ["id", "kind", "target", "severity", "detail"]) && u64(v.since_unix);
const backup = (v: unknown) => object(v) && text(v.path) && u64(v.bytes) && u64(v.taken_unix);

/**
 * Yalnız SİDECAR yanıtını doğrular. Tipli iptal burada geçmez: iptali yalnız
 * ana süreçteki yerel onay üretir (management-core). Sidecar'ın
 * `{cancelled:true}` yanıtı kabul edilseydi, sunucuda koşmuş bir işlem
 * "vazgeçildi, hiçbir şey değişmedi" diye gösterilirdi.
 */
export function validateManagementResult(method: string, value: unknown): unknown {
  let valid = false;
  if (object(value)) {
    switch (method) {
      case "apps.list": valid = array(value.apps, app); break;
      case "apps.get": valid = app(value.app) && array(value.releases, release) && (value.active_release_id === undefined || text(value.active_release_id)); break;
      case "apps.create": case "apps.update": valid = app(value.app) && ["proxy_detail", "env_detail", "volume_detail"].every((k) => value[k] === undefined || text(value[k])); break;
      case "apps.deploy": valid = texts(value, ["release_id", "image_id", "output"]) && value.release_id !== "" && value.image_id !== "" && typeof value.truncated === "boolean"; break;
      case "apps.logs": valid = text(value.output) && typeof value.truncated === "boolean"; break;
      case "apps.rollback": valid = texts(value, ["app_id", "from_release_id", "to_release_id"]) && value.to_release_id !== "" && typeof value.recreated === "boolean"; break;
      case "apps.delete": valid = text(value.app_id) && uint(value.containers_removed) && uint(value.releases_deleted) && uint(value.deployments_deleted) && strings(value.volumes_kept); break;
      case "alarms.list": valid = array(value.alarms, alarm) && typeof value.delivery_is_local_only === "boolean"; break;
      case "backups.list": valid = array(value.backups, backup) && uint(value.keep); break;
      case "backups.create": valid = backup(value.backup) && typeof value.volumes_excluded === "boolean"; break;
    }
  }
  if (!valid) throw new Error(`invalid ${method} response; refresh and check the server version`);
  return value;
}

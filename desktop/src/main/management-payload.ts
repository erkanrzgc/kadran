/**
 * create / update yükünün iç içe alanlarının ana süreçte doğrulanması.
 *
 * KURAL: sunucunun kurallarını AYNEN yansıt, ASLA daha sıkı olma
 * (internal/api/appvalidate.go ve volumevalidate.go). Daha sıkı bir kural
 * meşru uygulamaları kilitler. Daha gevşek olması ise zararsız: asıl
 * otorite sunucu ve executor'dır; burada amaç, ele geçmiş bir renderer'ın
 * onay kutusuna yanlış şey yazdırmasını ve argüman enjeksiyonu biçimlerini
 * ana süreçte kesmektir. Protojson bilinmeyen alanı reddettiği için burada
 * da bilinmeyen alan reddedilir.
 */

type Obj = Record<string, unknown>;

export const BRANCH_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$/;
const HOST = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$/;
const PATH_SEGMENT = /^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$/;
const BUILD_ARG_NAME = /^[A-Za-z_][A-Za-z0-9_]*$/;
const VOLUME_NAME = /^[a-z0-9][a-z0-9-]{0,63}$/;
const MAX_REPLICAS = 64;
const MAX_BUILD_ARGS = 100;
const MAX_BUILD_ARG_BYTES = 32 << 10;
const MAX_DOMAIN = 253;
const MAX_HEALTH_PATH = 512;
const MAX_DOCKERFILE = 4096;
const MAX_ENV_ENTRIES = 200;
const MAX_ENV_BYTES = 32 << 10;
const MAX_ENV_KEY_BYTES = 256;
const MAX_VOLUMES = 16;
const MAX_MOUNT_PATH = 4096;
const FORBIDDEN_MOUNT_ROOTS = ["/proc", "/sys", "/dev"];
const UINT32_MAX = 4294967295;
const UINT64_MAX = 18446744073709551615n;

const CREATE_ONLY_KEYS = ["app_id", "git_host", "git_owner", "git_repo", "dockerfile_path", "build_args", "container_port", "limits"];

const bad = (message: string) => new Error(message);
const isObj = (v: unknown): v is Obj => typeof v === "object" && v !== null && !Array.isArray(v);
const bytes = (s: string) => Buffer.byteLength(s, "utf8");

/** protojson'daki gibi: yok ya da null → varsayılan (boş metin). */
function str(v: unknown, field: string): string {
  if (v === undefined || v === null) return "";
  if (typeof v !== "string") throw bad(`${field} must be a string`);
  return v;
}

/** protojson sayıyı JSON sayısı ya da onluk metin olarak kabul eder. */
function uint(v: unknown, field: string, min: number, max: number): number {
  const n = v === undefined || v === null ? 0 : typeof v === "string" && /^\d{1,15}$/.test(v) ? Number(v) : v;
  if (typeof n !== "number" || !Number.isSafeInteger(n) || n < min || n > max) throw bad(`${field} must be an integer from ${min} to ${max}`);
  return n;
}

function uint64Positive(v: unknown, field: string): void {
  let n: bigint | null = null;
  if (typeof v === "string" && /^\d{1,20}$/.test(v)) n = BigInt(v);
  else if (typeof v === "number" && Number.isSafeInteger(v) && v >= 0) n = BigInt(v);
  if (n === null || n === 0n || n > UINT64_MAX) throw bad(`${field} must be a positive integer`);
}

function onlyKeys(value: Obj, allowed: string[], what: string): void {
  if (Object.keys(value).some((key) => !allowed.includes(key))) throw bad(`unknown ${what} field`);
}

/** Go path.Clean'in sözcüksel karşılığı (konteyner ve depo yolları POSIX). */
export function goClean(p: string): string {
  if (p === "") return ".";
  const rooted = p.startsWith("/");
  const out: string[] = [];
  for (const segment of p.split("/")) {
    if (segment === "" || segment === ".") continue;
    if (segment === "..") {
      if (out.length > 0 && out[out.length - 1] !== "..") out.pop();
      else if (!rooted) out.push("..");
    } else out.push(segment);
  }
  const joined = out.join("/");
  return rooted ? "/" + joined : joined === "" ? "." : joined;
}

function checkDomain(value: unknown, field: string): void {
  const d = str(value, field);
  if (d === "") return;
  if (bytes(d) > MAX_DOMAIN || !HOST.test(d)) throw bad(`invalid ${field}`);
}

function checkHealthPath(value: unknown): void {
  const p = str(value, "health_path");
  if (p === "") return;
  if (bytes(p) > MAX_HEALTH_PATH || !p.startsWith("/") || /[ \t\r\n]/.test(p)) throw bad("invalid health_path");
}

function checkBranch(value: unknown): void {
  if (!BRANCH_PATTERN.test(str(value, "git_branch"))) throw bad("invalid git_branch");
}

function checkSource(spec: Obj): void {
  const host = str(spec.git_host, "git_host");
  if (host === "" || bytes(host) > MAX_DOMAIN || !HOST.test(host)) throw bad("invalid git_host");
  for (const field of ["git_owner", "git_repo"]) {
    const segment = str(spec[field], field);
    if (!PATH_SEGMENT.test(segment) || segment === "." || segment === "..") throw bad(`invalid ${field}`);
  }
  checkBranch(spec.git_branch);
}

function checkDockerfile(value: unknown): void {
  const p = str(value, "dockerfile_path");
  if (p === "") return;
  if (bytes(p) > MAX_DOCKERFILE || p.startsWith("/") || goClean(p) !== p || p === "." || p.startsWith("../") || p.includes("\\")) {
    throw bad("invalid dockerfile_path");
  }
}

function checkBuildArgs(value: unknown): void {
  if (value === undefined || value === null) return;
  if (!isObj(value)) throw bad("build_args must be an object");
  const entries = Object.entries(value);
  if (entries.length > MAX_BUILD_ARGS) throw bad("too many build args");
  for (const [k, v] of entries) {
    if (!BUILD_ARG_NAME.test(k)) throw bad("invalid build arg name");
    if (typeof v !== "string" || bytes(v) > MAX_BUILD_ARG_BYTES || v.includes("\0")) throw bad("invalid build arg value");
  }
}

function checkEnv(value: unknown): Obj {
  if (value === undefined || value === null) return {};
  if (!isObj(value)) throw bad("env must be an object");
  const entries = Object.entries(value);
  if (entries.length > MAX_ENV_ENTRIES) throw bad("too many environment variables");
  let total = 0;
  for (const [k, v] of entries) {
    if (bytes(k) > MAX_ENV_KEY_BYTES || !BUILD_ARG_NAME.test(k)) throw bad("invalid environment variable name");
    if (typeof v !== "string" || v.includes("\0")) throw bad("invalid environment variable value");
    total += bytes(k) + bytes(v) + 1;
  }
  if (total > MAX_ENV_BYTES) throw bad("environment variables are too large");
  return value;
}

function checkMountPath(p: string): void {
  if (p === "" || bytes(p) > MAX_MOUNT_PATH || !p.startsWith("/") || goClean(p) !== p || p === "/") throw bad("invalid volume mount path");
  if (FORBIDDEN_MOUNT_ROOTS.some((root) => p === root || p.startsWith(root + "/"))) throw bad("invalid volume mount path");
}

const overlaps = (a: string, b: string) => a === b || a.startsWith(b + "/") || b.startsWith(a + "/");

function checkVolumes(value: unknown): string[] {
  if (value === undefined || value === null) return [];
  if (!Array.isArray(value)) throw bad("volumes must be a list");
  if (value.length > MAX_VOLUMES) throw bad("too many volumes");
  const names: string[] = [];
  const paths: string[] = [];
  for (const item of value) {
    if (!isObj(item)) throw bad("invalid volume");
    onlyKeys(item, ["name", "mount_path", "read_only"], "volume");
    const name = str(item.name, "volume name");
    if (!VOLUME_NAME.test(name) || names.includes(name)) throw bad("invalid volume name");
    if (item.read_only !== undefined && item.read_only !== null && typeof item.read_only !== "boolean") throw bad("volume read_only must be true or false");
    const mount = str(item.mount_path, "volume mount path");
    checkMountPath(mount);
    if (paths.some((previous) => overlaps(previous, mount))) throw bad("volume mount points overlap");
    names.push(name);
    paths.push(mount);
  }
  return names;
}

function checkNameList(value: unknown, field: string, pattern: RegExp, conflicts: (name: string) => boolean): void {
  if (value === undefined || value === null) return;
  if (!Array.isArray(value)) throw bad(`${field} must be a list`);
  for (const name of value) {
    if (typeof name !== "string" || !pattern.test(name)) throw bad(`invalid name in ${field}`);
    if (conflicts(name)) throw bad(`${field} conflicts with the values being set`);
  }
}

function checkLimits(value: unknown): void {
  if (!isObj(value)) throw bad("limits are required");
  onlyKeys(value, ["memory_bytes", "cpu_millis", "blkio_weight"], "limits");
  uint64Positive(value.memory_bytes, "limits.memory_bytes");
  uint(value.cpu_millis, "limits.cpu_millis", 1, UINT32_MAX);
  uint(value.blkio_weight, "limits.blkio_weight", 10, 1000);
}

/** apps.create yükü: tam AppSpec. */
export function validateCreateSpec(spec: Obj): void {
  checkSource(spec);
  checkDockerfile(spec.dockerfile_path);
  checkBuildArgs(spec.build_args);
  checkEnv(spec.env);
  checkVolumes(spec.volumes);
  uint(spec.container_port, "container_port", 1, 65535);
  uint(spec.replicas, "replicas", 1, MAX_REPLICAS);
  checkHealthPath(spec.health_path);
  checkDomain(spec.domain, "domain");
  checkLimits(spec.limits);
}

/** apps.update yükü: yalnız verilen alanlar; sunucu birleşik tanımı ayrıca doğrular. */
export function validateAppUpdate(update: Obj): void {
  if (CREATE_ONLY_KEYS.some((key) => key in update)) throw bad("unknown app field");
  if (update.domain !== undefined && update.domain !== null) checkDomain(update.domain, "domain");
  if (update.git_branch !== undefined && update.git_branch !== null) checkBranch(update.git_branch);
  if (update.health_path !== undefined && update.health_path !== null) checkHealthPath(update.health_path);
  if (update.replicas !== undefined && update.replicas !== null) uint(update.replicas, "replicas", 1, MAX_REPLICAS);
  const env = checkEnv(update.env);
  const volumeNames = checkVolumes(update.volumes);
  checkNameList(update.env_remove, "env_remove", BUILD_ARG_NAME, (name) => Object.hasOwn(env, name));
  checkNameList(update.volume_remove, "volume_remove", VOLUME_NAME, (name) => volumeNames.includes(name));
}

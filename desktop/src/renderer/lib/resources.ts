/**
 * Canlı kaynak kullanımı (K-166). Sunucu kümülatif sayaç ve ölçüm anı
 * dönüyor; hızlar (CPU %, bayt/sn) burada iki ölçümün farkından çıkıyor.
 *
 * İki kural:
 * - Ölçülemeyen değer null'dır, 0 değil. Sunucu okuyamadığı alanı hiç
 *   göndermiyor; bellek bir kez sessizce 0 raporlandı.
 * - Sayaç geri giderse (host ya da konteyner yeniden başladı, dağıtım) o
 *   aralığın hızı null'dır: dev bir sıçrama ya da eksi bir değer yanlıştır.
 */

export interface HostCounters {
  cpuCount: number;
  cpuTotal: number | null;
  cpuIdle: number | null;
  cpuIowait: number | null;
  cpuSteal: number | null;
  memoryTotal: number | null;
  memoryAvailable: number | null;
  swapTotal: number | null;
  swapFree: number | null;
  load1: number | null;
  load5: number | null;
  load15: number | null;
  tasks: number | null;
  diskTotal: number | null;
  diskAvailable: number | null;
  inodesTotal: number | null;
  inodesFree: number | null;
  diskRead: number | null;
  diskWrite: number | null;
  netRx: number | null;
  netTx: number | null;
  cpuPressure: number | null;
  memoryPressure: number | null;
  ioPressure: number | null;
  uptime: number | null;
}

export interface AppCounters {
  appId: string;
  containers: number;
  cpuNs: number;
  memory: number;
  memoryLimit: number;
  netRx: number;
  netTx: number;
  diskRead: number;
  diskWrite: number;
  tasks: number;
}

export interface ResourceSnapshot {
  /** Sunucunun ölçüm anı, Unix ms. */
  at: number;
  host: HostCounters;
  /** null: istenmedi ya da ölçülemedi (executor kapalı). Boş dizi: çalışan uygulama yok. */
  apps: AppCounters[] | null;
}

export interface AppRates {
  /** Hostun yüzdesi (0–100). */
  cpu: number | null;
  netRx: number | null;
  netTx: number | null;
  diskRead: number | null;
  diskWrite: number | null;
}

export interface ResourceRates {
  /** Yüzde (0–100). */
  cpu: number | null;
  iowait: number | null;
  steal: number | null;
  /** Bayt/sn. */
  netRx: number | null;
  netTx: number | null;
  diskRead: number | null;
  diskWrite: number | null;
  apps: ReadonlyMap<string, AppRates>;
}

const INVALID = "invalid resource usage";

type Raw = Record<string, unknown>;

function record(value: unknown): Raw {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error(INVALID);
  return value as Raw;
}

/** 64 bit sayı: protojson dize olarak yazıyor. Negatif ya da bozuk değer reddedilir. */
function counter(value: unknown): number {
  if (typeof value === "number" && Number.isFinite(value) && value >= 0) return value;
  if (typeof value === "string" && /^\d+$/.test(value)) return Number(value);
  throw new Error(INVALID);
}

/** 32 bit sayı ve double: protojson sayı olarak yazıyor. */
function plain(value: unknown): number {
  if (typeof value === "number" && Number.isFinite(value) && value >= 0) return value;
  throw new Error(INVALID);
}

function optional(raw: Raw, key: string, read: (v: unknown) => number): number | null {
  return raw[key] === undefined || raw[key] === null ? null : read(raw[key]);
}

function parseHost(value: unknown): HostCounters {
  const h = record(value);
  const c = (key: string) => optional(h, key, counter);
  const p = (key: string) => optional(h, key, plain);
  return {
    cpuCount: optional(h, "cpu_count", plain) ?? 0,
    cpuTotal: c("cpu_total_ticks"),
    cpuIdle: c("cpu_idle_ticks"),
    cpuIowait: c("cpu_iowait_ticks"),
    cpuSteal: c("cpu_steal_ticks"),
    memoryTotal: c("memory_total_bytes"),
    memoryAvailable: c("memory_available_bytes"),
    swapTotal: c("swap_total_bytes"),
    swapFree: c("swap_free_bytes"),
    load1: p("load1"),
    load5: p("load5"),
    load15: p("load15"),
    tasks: p("tasks"),
    diskTotal: c("disk_total_bytes"),
    diskAvailable: c("disk_available_bytes"),
    inodesTotal: c("inodes_total"),
    inodesFree: c("inodes_free"),
    diskRead: c("disk_read_bytes"),
    diskWrite: c("disk_write_bytes"),
    netRx: c("net_rx_bytes"),
    netTx: c("net_tx_bytes"),
    cpuPressure: p("cpu_pressure"),
    memoryPressure: p("memory_pressure"),
    ioPressure: p("io_pressure"),
    uptime: c("uptime_seconds"),
  };
}

function parseApp(value: unknown): AppCounters {
  const a = record(value);
  if (typeof a.app_id !== "string" || a.app_id === "") throw new Error(INVALID);
  const c = (key: string) => optional(a, key, counter) ?? 0;
  const p = (key: string) => optional(a, key, plain) ?? 0;
  return {
    appId: a.app_id,
    containers: p("containers"),
    cpuNs: c("cpu_usage_ns"),
    memory: c("memory_bytes"),
    memoryLimit: c("memory_limit_bytes"),
    netRx: c("net_rx_bytes"),
    netTx: c("net_tx_bytes"),
    diskRead: c("disk_read_bytes"),
    diskWrite: c("disk_write_bytes"),
    tasks: p("tasks"),
  };
}

/** Köprünün yanıtı; biçim bozuksa fırlatır (renderer kendini korur). */
export function parseResources(raw: unknown): ResourceSnapshot {
  const r = record(raw);
  const at = counter(r.sampled_at_unix_ms);
  const host = parseHost(r.host);
  let apps: AppCounters[] | null = null;
  if (r.apps_measured === true) {
    if (!Array.isArray(r.apps)) throw new Error(INVALID);
    apps = r.apps.map(parseApp);
  } else if (r.apps_measured !== false) {
    throw new Error(INVALID);
  }
  return { at, host, apps };
}

/** Saniyedeki artış; sayaç geri gittiyse ya da bilinmiyorsa null. */
function perSecond(prev: number | null, next: number | null, seconds: number): number | null {
  if (prev === null || next === null || next < prev) return null;
  return (next - prev) / seconds;
}

/** Toplamın yüzdesi; payda yoksa ya da pay geri gittiyse null. */
function share(prev: number | null, next: number | null, total: number): number | null {
  if (prev === null || next === null || next < prev || total <= 0) return null;
  return ((next - prev) / total) * 100;
}

const EMPTY_APP: AppRates = { cpu: null, netRx: null, netTx: null, diskRead: null, diskWrite: null };

function appRates(prev: AppCounters | undefined, next: AppCounters, seconds: number, cores: number): AppRates {
  // Replika sayısı değiştiyse toplam başka konteynerlerin toplamı: o aralık bilinmiyor.
  if (!prev || prev.containers !== next.containers) return EMPTY_APP;
  const hostNs = seconds * 1e9 * cores;
  return {
    cpu: share(prev.cpuNs, next.cpuNs, hostNs),
    netRx: perSecond(prev.netRx, next.netRx, seconds),
    netTx: perSecond(prev.netTx, next.netTx, seconds),
    diskRead: perSecond(prev.diskRead, next.diskRead, seconds),
    diskWrite: perSecond(prev.diskWrite, next.diskWrite, seconds),
  };
}

/** İki ölçüm arasındaki hızlar. İlk ölçümde (prev null) hepsi null. */
export function rates(prev: ResourceSnapshot | null, next: ResourceSnapshot): ResourceRates {
  const seconds = prev ? (next.at - prev.at) / 1000 : 0;
  if (!prev || seconds <= 0) {
    return { cpu: null, iowait: null, steal: null, netRx: null, netTx: null, diskRead: null, diskWrite: null, apps: new Map() };
  }
  const p = prev.host;
  const n = next.host;
  const dTotal = p.cpuTotal !== null && n.cpuTotal !== null ? n.cpuTotal - p.cpuTotal : 0;
  const idle = share(p.cpuIdle, n.cpuIdle, dTotal);
  const apps = new Map<string, AppRates>();
  if (prev.apps && next.apps) {
    const before = new Map(prev.apps.map((a) => [a.appId, a]));
    for (const a of next.apps) apps.set(a.appId, appRates(before.get(a.appId), a, seconds, n.cpuCount));
  }
  return {
    cpu: idle === null || idle > 100 ? null : 100 - idle,
    iowait: share(p.cpuIowait, n.cpuIowait, dTotal),
    steal: share(p.cpuSteal, n.cpuSteal, dTotal),
    netRx: perSecond(p.netRx, n.netRx, seconds),
    netTx: perSecond(p.netTx, n.netTx, seconds),
    diskRead: perSecond(p.diskRead, n.diskRead, seconds),
    diskWrite: perSecond(p.diskWrite, n.diskWrite, seconds),
    apps,
  };
}

/**
 * Filo görünümünün saf mantığı. Kart verisi sunucunun bugünkü çağrılarından
 * gelir (status, apps.list, alarms.list); yeni sunucu RPC'si yok.
 *
 * Okunamayan alan "yok" diye gösterilmez: alarm listesi gelmediyse kart
 * "alarm yok" iddia etmez, sayı null kalır ve kart uyarıya düşer.
 */

import type { Profile } from "../bridge.ts";
import type { Tone } from "./chain.ts";
import type { AlarmsPayload, AppsListPayload, AuditRecord, HostInfo, StatusPayload } from "./types.ts";

export type CardState = "loading" | "ready" | "totp" | "unreachable";

export interface CardData {
  status?: StatusPayload;
  apps?: AppsListPayload;
  alarms?: AlarmsPayload;
  /** status çağrısının hata metni. */
  failure?: string;
}

export interface FleetCard {
  profile: Profile;
  state: CardState;
  tone: Tone;
  status: StatusPayload | null;
  apps: AppsListPayload | null;
  alarms: AlarmsPayload | null;
  appCount: number | null;
  alarmCount: number | null;
  failure: string | null;
}

export interface ProviderGroup {
  provider: string;
  cards: FleetCard[];
}

export interface FleetSummary {
  servers: number;
  ready: number;
  warning: number;
  unavailable: number;
  apps: number;
}

export interface ServerRecords {
  target: string;
  records: readonly AuditRecord[];
}

export interface FleetOperation {
  server: string;
  record: AuditRecord;
}

/** Sunucu değiştiren ve geçmişi olan işlemler; yükseltmeler sunucuda kayıt bırakmıyor. */
export const OPERATION_ACTIONS: readonly string[] = ["app.deploy", "app.rollback"];

/** Sunucunun TOTP mesajı "kadran login" der; hata metni tek ortak sinyal. */
export function classifyFailure(message: string): "totp" | "unreachable" {
  return /totp|kadran login/i.test(message) ? "totp" : "unreachable";
}

function toneOf(state: CardState, data: CardData): Tone {
  if (state === "unreachable") return "bad";
  if (state !== "ready") return "muted";
  const healthy = data.status?.executor_reachable === true && data.alarms !== undefined && data.alarms.alarms.length === 0;
  return healthy ? "ok" : "warn";
}

export function cardOf(profile: Profile, data: CardData): FleetCard {
  const state: CardState = data.failure !== undefined ? classifyFailure(data.failure) : data.status ? "ready" : "loading";
  return {
    profile,
    state,
    tone: toneOf(state, data),
    status: data.status ?? null,
    apps: data.apps ?? null,
    alarms: data.alarms ?? null,
    appCount: data.apps ? data.apps.apps.length : null,
    alarmCount: data.alarms ? data.alarms.alarms.length : null,
    failure: data.failure ?? null,
  };
}

export function alarmCount(cards: readonly FleetCard[]): number {
  return cards.reduce((sum, card) => sum + (card.alarmCount ?? 0), 0);
}

export function diskUsedPercent(host: HostInfo | null): number | null {
  if (!host) return null;
  const total = Number(host.disk_total_bytes);
  const available = Number(host.disk_available_bytes);
  if (!Number.isFinite(total) || !Number.isFinite(available) || total <= 0) return null;
  return Math.round(((total - available) / total) * 100);
}

/** Gruplar ilk görülme sırasında; etiketsiz sunucular en sonda. */
export function groupByProvider(cards: readonly FleetCard[]): ProviderGroup[] {
  const order = new Map<string, FleetCard[]>();
  for (const card of cards) {
    const key = card.profile.provider ?? "";
    order.set(key, [...(order.get(key) ?? []), card]);
  }
  const groups = [...order].map(([provider, list]) => ({ provider, cards: list }));
  return [...groups.filter((g) => g.provider !== ""), ...groups.filter((g) => g.provider === "")];
}

export function summarize(cards: readonly FleetCard[]): FleetSummary {
  return {
    servers: cards.length,
    ready: cards.filter((c) => c.tone === "ok").length,
    warning: cards.filter((c) => c.tone === "warn").length,
    unavailable: cards.filter((c) => c.state === "totp" || c.state === "unreachable").length,
    apps: cards.reduce((sum, c) => sum + (c.appCount ?? 0), 0),
  };
}

function semver(version: string): readonly [number, number, number] | null {
  const match = /^v?(\d+)\.(\d+)\.(\d+)/.exec(version);
  return match ? [Number(match[1]), Number(match[2]), Number(match[3])] : null;
}

/** Sunucu istemciden eski mi? Etiketsiz derlemede ("dev") karşılaştırma anlamsız. */
export function updateAvailable(server: string, client: string): boolean {
  const a = semver(server);
  const b = semver(client);
  if (!a || !b) return false;
  for (const i of [0, 1, 2] as const) {
    if (a[i] !== b[i]) return a[i] < b[i];
  }
  return false;
}

const timeOf = (record: AuditRecord): number => {
  const value = record.ts ? new Date(record.ts).getTime() : Number.NaN;
  return Number.isNaN(value) ? Number.NEGATIVE_INFINITY : value;
};

/** Bütün sunucuların kayıtları, en yeni üstte. `actions` null ise süzmez. */
export function mergeOperations(servers: readonly ServerRecords[], actions: readonly string[] | null, limit: number): FleetOperation[] {
  return servers
    .flatMap(({ target, records }) =>
      records.filter((r) => actions === null || actions.includes(r.action)).map((record) => ({ server: target, record })),
    )
    .sort((a, b) => timeOf(b.record) - timeOf(a.record))
    .slice(0, limit);
}

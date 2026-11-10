/**
 * Sayı, süre ve kimlik biçimleri. CLI ile birebir aynı (cmd/kadran/format.go):
 * kullanıcı aynı değeri iki araçta farklı okumamalı.
 */

import type { Actor } from "./types.ts";

const UNITS: ReadonlyArray<readonly [number, string]> = [
  [86_400, "d"],
  [3_600, "h"],
  [60, "m"],
  [1, "s"],
];

/** humanDuration, saniyeyi en çok iki birimle yazar: "4d 7h". */
export function humanDuration(seconds: number | string): string {
  const total = Math.floor(Number(seconds));
  if (!Number.isFinite(total) || total < 1) return "0s";
  const parts: string[] = [];
  let rest = total;
  for (const [size, label] of UNITS) {
    if (rest < size) continue;
    parts.push(`${Math.floor(rest / size)}${label}`);
    rest %= size;
    if (parts.length === 2) break;
  }
  return parts.join(" ");
}

/** humanBytes, CLI'daki gibi 1024 tabanlı yazar: "3.7 GiB". */
export function humanBytes(value: number | string): string {
  const n = Number(value);
  if (!Number.isFinite(n) || n < 0) return "—";
  if (n < 1024) return `${Math.floor(n)} B`;
  let div = 1024;
  let exp = 0;
  for (let v = Math.floor(n / 1024); v >= 1024; v = Math.floor(v / 1024)) {
    div *= 1024;
    exp++;
  }
  // Go's %.1f yuvarlar tam yarımda çift sayıya (banker's rounding); değer eşleştirmeliyiz.
  const scaled = (n / div) * 10;
  let k: number;
  if (Number.isInteger(scaled * 2) && !Number.isInteger(scaled)) {
    // Exact .5 tie: round to nearest even
    k = Math.round(scaled / 2) * 2;
  } else {
    k = Math.round(scaled);
  }
  return `${(k / 10).toFixed(1)} ${"KMGTPE"[exp]}iB`;
}

/** shortFingerprint, uzun SSH parmak izini listede okunur kılar. */
export function shortFingerprint(fp: string): string {
  const prefix = "SHA256:";
  const body = fp.startsWith(prefix) ? fp.slice(prefix.length) : fp;
  if (body.length <= 16) return fp;
  return `${prefix}${body.slice(0, 8)}…${body.slice(-6)}`;
}

/** describeActor, aktörü tek hücreye sığdırır; bilinmiyorsa null (arayüz çevirir). */
export function describeActor(actor: Actor | null): string | null {
  if (!actor) return null;
  if (actor.ssh_key_fingerprint) return shortFingerprint(actor.ssh_key_fingerprint);
  if (actor.label) return actor.label;
  if (actor.origin) return actor.origin;
  return null;
}

/** decimal: sayıyı arayüz dilinde sabit ondalıkla yazar ("0,42"). */
export function decimal(value: number, digits: number, locale: string): string {
  return value.toLocaleString(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits });
}

/** percentNumber: yüzdenin sayı kısmı; 10'un altında bir ondalık ("4,2"), üstünde tam ("37"). */
export function percentNumber(value: number, locale: string): string {
  return decimal(value, Math.round(value * 10) / 10 < 10 ? 1 : 0, locale);
}

/** formatTime, RFC 3339 zamanı arayüz dilinde yazar. */
export function formatTime(ts: string | null, locale: string): string {
  if (!ts) return "—";
  const d = new Date(ts);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString(locale);
}

/** formatUnix: saniye cinsinden sunucu zamanı. Bozuk ya da aralık dışıysa "—": arayüz düşmez. */
export function formatUnix(seconds: number | string, locale: string): string {
  const d = new Date(Number(seconds) * 1000);
  return Number.isNaN(d.getTime()) ? "—" : d.toLocaleString(locale);
}

const RELATIVE_STEPS: ReadonlyArray<readonly [number, Intl.RelativeTimeFormatUnit]> = [
  [86_400, "day"],
  [3_600, "hour"],
  [60, "minute"],
];

/** relativeTime: "2 gün önce". Zaman yok ya da bozuksa "—"; gelecekteki saat kayması "şimdi" olur. */
export function relativeTime(ts: string | null, now: number, locale: string): string {
  const then = ts ? new Date(ts).getTime() : Number.NaN;
  if (Number.isNaN(then)) return "—";
  const seconds = Math.max(0, Math.floor((now - then) / 1000));
  const format = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  for (const [size, unit] of RELATIVE_STEPS) {
    if (seconds >= size) return format.format(-Math.floor(seconds / size), unit);
  }
  return format.format(0, "second");
}

export function formatCount(n: number, locale: string): string {
  return n.toLocaleString(locale);
}

/**
 * versionLabel: sidecar'ın bildirdiği sürüm. CI etiketi olduğu gibi
 * damgalar ("v0.5.1"); başına bir v daha eklenince "vv0.5.1" oluyordu.
 * Rakamla başlayana v eklenir; "dev" gibi etiketsiz derleme olduğu gibi kalır.
 */
export function versionLabel(version: string): string {
  return /^\d+\.\d+/.test(version) ? `v${version}` : version;
}

/**
 * railVersion: kenar çubuğu için kısa sürüm. git describe'ın etiketten sonraki
 * commit sayısı, karması ve "-dirty" eki atılır; tamamı ipucunda ve Ayarlar'da.
 */
export function railVersion(version: string): string {
  return versionLabel(version.replace(/(-\d+-g[0-9a-f]+)?(-dirty)?$/, ""));
}

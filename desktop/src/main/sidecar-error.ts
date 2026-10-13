/**
 * Sidecar hatasından kullanıcıya gidecek metnin TEK kurucusu.
 *
 * Gerçek sidecar (cmd/kadran/sidecar.go) hatayı `{message: "call failed" |
 * "could not connect", data: "<gerçek sebep>"}` biçiminde döndürür; yalnız
 * `message`'ı göstermek kullanıcıya "call failed" demekti. Burada ikisi
 * birleştirilir, sır sızdırabilecek parçalar silinir ve uzunluk sınırlanır.
 * Renderer'a HAM `data` hiç gitmez: her IPC işleyicisi hatayı buradan geçirir.
 *
 * Sıra önemli: önce kontrol/biçim karakterleri atılır, SONRA sırlar silinir.
 * Tersi olsaydı `ghp_` içine sıfır genişlikli karakter sokan bir metin
 * redaksiyonu atlatırdı.
 */

import { SidecarError } from "../shared/sidecar-client.ts";

/** Kullanıcıya giden metnin en fazla kod noktası sayısı (sonuna "…" eklenir). */
export const MAX_ERROR_CHARS = 500;

const REDACTED = "***";

/** Satır sonu ve sekme boşluğa çevrilir; geri kalan kontrol (Cc) ve biçim (Cf: bidi, sıfır genişlik) karakterleri atılır. */
function stripUnsafeCharacters(text: string): string {
  return text.replace(/[\r\n\t]+/g, " ").replace(/[\p{Cc}\p{Cf}]/gu, "");
}

// Her kural adlı bir sabit: mutasyon ölçümü (scripts/mutate-desktop.sh) tek tek devre dışı bırakır.
const X_ACCESS_TOKEN = /x-access-token:[^@\s/]*@/gi;
const URL_USERINFO = /(\b[a-z][a-z0-9+.-]*:\/\/)[^/\s]*@/gi;
const GITHUB_PAT = /github_pat_[A-Za-z0-9_]+/g;
const GITHUB_TOKEN = /gh[pousr]_[A-Za-z0-9_]+/g;

/** Bağlantı adreslerindeki kimlik bilgisi ve GitHub jetonları silinir. */
export function redactSecrets(text: string): string {
  return text
    .replace(X_ACCESS_TOKEN, `x-access-token:${REDACTED}@`)
    .replace(URL_USERINFO, `$1${REDACTED}@`)
    .replace(GITHUB_PAT, REDACTED)
    .replace(GITHUB_TOKEN, REDACTED);
}

function limit(text: string): string {
  const chars = [...text];
  return chars.length > MAX_ERROR_CHARS ? `${chars.slice(0, MAX_ERROR_CHARS).join("")}…` : text;
}

/** Hatadan kullanıcıya gidecek güvenli metin: `message` + (varsa) `data`. */
export function sidecarErrorText(err: unknown): string {
  let raw: string;
  if (err instanceof SidecarError) raw = err.data ? `${err.message}: ${err.data}` : err.message;
  else if (err instanceof Error) raw = err.message;
  else raw = String(err);
  return limit(redactSecrets(stripUnsafeCharacters(raw)));
}

/** IPC'den fırlatılacak hata: Electron yalnız `message`'ı taşır, o da arıtılmış olur. */
export function userFacingError(err: unknown): Error {
  return new Error(sidecarErrorText(err));
}

/** Sidecar'a çağrı yapan işlev; `SidecarClient.call` ile uyumlu. */
export type SidecarCall = (method: string, params?: Record<string, unknown>, options?: { timeoutMs: number }) => Promise<unknown>;

/** Sidecar çağrısı; reddedilirse renderer'a giden hata arıtılmış olur. */
export function callForUi(call: SidecarCall, method: string, params?: Record<string, unknown>, options?: { timeoutMs: number }): Promise<unknown> {
  return call(method, params, options).catch((err: unknown) => { throw userFacingError(err); });
}

/**
 * Yönetim işlemlerinde kullanıcı ana süreçteki yerel onay kutusunda
 * vazgeçerse hata DEĞİL, bu tipli sonuç döner. Renderer'da tek bir yer
 * (useManagementOperation) bunu işler.
 */

export interface ManagementCancelled {
  readonly cancelled: true;
}

export type Cancellable<T> = T | ManagementCancelled;

export function cancelled(): ManagementCancelled {
  return { cancelled: true };
}

/** Yalnız tam biçimi kabul eder: başka alan taşıyan bir nesne iptal sayılmaz. */
export function isCancelled(value: unknown): value is ManagementCancelled {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const keys = Object.keys(value);
  return keys.length === 1 && keys[0] === "cancelled" && (value as Record<string, unknown>).cancelled === true;
}

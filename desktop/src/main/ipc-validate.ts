/**
 * IPC girdilerinin doğrulanması. Renderer güvenilmez bir kaynak:
 * contextIsolation onu Node'dan ayırıyor ama gönderdiği veriyi
 * doğrulamıyor. Hata iletileri İngilizce: arayüze ulaşıyorlar (K-141).
 */

export const MAX_TARGET = 500;
export const MAX_COPY = 1024;
export const WINDOW_ACTIONS = ["minimize", "maximize", "close"] as const;
export type WindowAction = (typeof WINDOW_ACTIONS)[number];

export function requireTarget(value: unknown): string {
  if (typeof value !== "string" || value.trim() === "") throw new Error("target is empty");
  if (value.length > MAX_TARGET) throw new Error("target is too long");
  const trimmed = value.trim();
  // Denetim (\p{Cc}) ve biçim (\p{Cf}, bidi dahil) karakterleri yerel onay kutusunda
  // "Server:" satırını yanıltabilir; ssh hedefinde zaten anlamsızlar.
  if (/[\p{Cc}\p{Cf}]/u.test(trimmed)) throw new Error("target must not contain control or invisible characters");
  // ssh seçeneği olarak okunur (-oProxyCommand=…): savunma derinliği, Go tarafı da reddeder.
  if (trimmed.startsWith("-")) throw new Error('target must not start with "-"');
  return trimmed;
}

export function toUint(value: unknown, fallback: number): number {
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(n) || n < 0) return fallback;
  return Math.floor(n);
}

export function requireWindowAction(value: unknown): WindowAction {
  if (typeof value === "string" && (WINDOW_ACTIONS as readonly string[]).includes(value)) {
    return value as WindowAction;
  }
  throw new Error("unknown window action");
}

export function requireCopyText(value: unknown): string {
  if (typeof value !== "string" || value.length === 0 || value.length > MAX_COPY) {
    throw new Error("copy text must be 1-1024 characters");
  }
  return value;
}

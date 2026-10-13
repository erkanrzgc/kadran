/**
 * Oturum izinleri.
 *
 * Electron, işleyici kurulmazsa her izin isteğini onaylar. Renderer'ın
 * kamera, mikrofon, bildirim, konum ya da pano erişimine ihtiyacı yok.
 * Tek gerekli şey kurulu yazı tiplerinin listesi (Görünüm ayarları).
 *
 * Ölçüldü (Electron 43, Windows 11): Chromium bu listeyi İSTEK değil
 * KONTROL işleyicisinden "local-fonts" adıyla soruyor; ad Electron'un
 * TypeScript birleşim tipinde yok, bu yüzden karşılaştırma string
 * üzerinden. Reddedilirse queryLocalFonts() boş liste döndürür; arayüz
 * bunu "listelenemedi" olarak gösterir.
 *
 * Pano izni AÇILMAZ: parmak izi kopyalama köprü üzerinden ana süreçte
 * yapılır (copyText).
 */

import type { Session } from "electron";

export const ALLOWED_CHECKS: ReadonlySet<string> = new Set(["local-fonts"]);

export function allowPermissionCheck(permission: string): boolean {
  return ALLOWED_CHECKS.has(permission);
}

export function allowPermissionRequest(_permission: string): boolean {
  return false;
}

export type PermissionSession = Pick<Session, "setPermissionCheckHandler" | "setPermissionRequestHandler">;

export function lockDownPermissions(ses: PermissionSession): void {
  ses.setPermissionCheckHandler((_wc, permission) => allowPermissionCheck(permission as string));
  ses.setPermissionRequestHandler((_wc, permission, callback) => callback(allowPermissionRequest(permission as string)));
}

/**
 * İzinler. Electron işleyici kurulmazsa HER izni verir; bundan sonra
 * yalnız yazı tipi listesine izin var.
 *
 * Ölçüldü (Electron 43, Windows 11): Chromium yazı tipi listesini İSTEK
 * değil KONTROL işleyicisinden "local-fonts" adıyla soruyor; bu ad
 * Electron'un TypeScript tiplerinde yok. Reddedilince queryLocalFonts()
 * hata değil BOŞ liste döndürüyor.
 */

import { test } from "node:test";
import assert from "node:assert/strict";

import {
  allowPermissionCheck,
  allowPermissionRequest,
  lockDownPermissions,
  type PermissionSession,
} from "../src/main/permissions.ts";

test("kontrol: yalnız local-fonts", () => {
  assert.equal(allowPermissionCheck("local-fonts"), true);
  for (const p of ["notifications", "media", "geolocation", "clipboard-read", "clipboard-sanitized-write", "fullscreen", "openExternal", "", "LOCAL-FONTS"]) {
    assert.equal(allowPermissionCheck(p), false, p);
  }
});

test("istek: hiçbiri", () => {
  for (const p of ["local-fonts", "notifications", "media", "unknown"]) {
    assert.equal(allowPermissionRequest(p), false, p);
  }
});

test("lockDownPermissions iki işleyiciyi de kurar ve onlar kurala uyar", () => {
  let check: ((wc: unknown, permission: string) => boolean) | null = null;
  let request: ((wc: unknown, permission: string, cb: (granted: boolean) => void) => void) | null = null;
  const fake = {
    setPermissionCheckHandler: (h: typeof check) => {
      check = h;
    },
    setPermissionRequestHandler: (h: typeof request) => {
      request = h;
    },
  } as unknown as PermissionSession;

  lockDownPermissions(fake);
  assert.ok(check && request, "işleyiciler kurulmadı");
  assert.equal(check!(null, "local-fonts"), true);
  assert.equal(check!(null, "notifications"), false);
  let granted: boolean | null = null;
  request!(null, "media", (g) => {
    granted = g;
  });
  assert.equal(granted, false);
});

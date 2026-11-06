/**
 * Kayıtlı sunucu listesi (profiller).
 *
 * - Liste ortak dosyada (servers.json, K-162) ve onu yan süreç yazar. Arayüz
 *   bütün listeyi geri yazmaz, tek değişikliği gönderir: CLI'ın o arada
 *   eklediği sunucu silinmez. Her çağrı güncel listeyi döner, o uygulanır.
 * - Okuma ve yazmalar tek sırada (lib/serial.ts): yanıtlar gönderildikleri
 *   sırayla uygulanır, eski bir yanıt yenisinin üstüne yazılmaz.
 * - reload her zaman yeniden okur: pencere odağa gelince CLI'da yapılan
 *   değişiklik görünür.
 *
 * Hatalar çağırana döner; bildirimi çağıran gösterir.
 */

import type { Profile } from "../bridge.ts";
import { createSerial } from "./serial.ts";

export interface ProfileChange {
  name: string;
  provider: string;
}

export interface ProfileIO {
  load: () => Promise<Profile[]>;
  add: (profile: Profile) => Promise<Profile[]>;
  /** Hedefle bulunan kaydın adı ve sağlayıcısı; boş sağlayıcı alanı siler. */
  update: (target: string, change: ProfileChange) => Promise<Profile[]>;
  remove: (target: string) => Promise<Profile[]>;
  /** Okunan ya da değişiklikten dönen liste. */
  onChange: (profiles: Profile[]) => void;
}

export interface ProfileStore {
  /** Diskten okur; okunmuşsa yeniden okumaz. */
  load: () => Promise<void>;
  /** Her zaman yeniden okur. */
  reload: () => Promise<void>;
  add: (profile: Profile) => Promise<void>;
  remove: (target: string) => Promise<void>;
  /** Adı ve sağlayıcıyı değiştirir; hedef sabittir. */
  update: (target: string, change: ProfileChange) => Promise<void>;
}

export function createProfileStore(io: ProfileIO): ProfileStore {
  const serial = createSerial();
  let loaded = false;

  const read = async (): Promise<void> => {
    io.onChange(await io.load());
    loaded = true;
  };

  return {
    load: () =>
      serial(async () => {
        if (!loaded) await read();
      }),
    reload: () => serial(read),
    add: (profile) => serial(async () => io.onChange(await io.add(profile))),
    remove: (target) => serial(async () => io.onChange(await io.remove(target))),
    update: (target, change) => serial(async () => io.onChange(await io.update(target, change))),
  };
}

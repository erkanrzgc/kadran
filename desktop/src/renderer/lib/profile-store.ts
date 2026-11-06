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
 * Hatalar çağırana döner; bildirimi çağıran gösterir. "Kayıt yok" (CLI'da
 * silinmiş) listenin eskidiğini söyler: liste yeniden okunur.
 */

import type { Profile } from "../bridge.ts";
import { serverListReason } from "./errors.ts";
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
    // Kayıt CLI'da zaten silinmişse amaç olmuştur: güncel liste okunur, kart
    // gider. (serial içinde read; reload kendi sırasını beklerdi.)
    remove: (target) =>
      serial(async () => {
        try {
          io.onChange(await io.remove(target));
        } catch (err) {
          if (serverListReason(err) !== "not_found") throw err;
          await read();
        }
      }),
    // Düzenlenen kayıt silinmişse liste tazelenir ama hata yine pencerede söylenir.
    update: (target, change) =>
      serial(async () => {
        try {
          io.onChange(await io.update(target, change));
        } catch (err) {
          if (serverListReason(err) === "not_found") await read().catch(() => undefined);
          throw err;
        }
      }),
  };
}

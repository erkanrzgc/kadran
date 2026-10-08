/**
 * Kayıtlı sunucu listesi (profiller).
 *
 * - Diskten okunmadan yazılmaz: liste boş başlar; okuma bitmeden yapılan
 *   bir ekleme [yeni] kaydedip kullanıcının bütün sunucularını silerdi.
 *   Okuma başarısızsa yazma da yapılmaz, sonraki iş yeniden okumayı dener.
 * - Okuma ve yazmalar tek sırada (lib/serial.ts) ve hep en son listeden
 *   hesaplanır: üst üste iki yazma birbirinin değişikliğini silemez.
 *
 * Hatalar çağırana döner; bildirimi çağıran gösterir.
 */

import type { Profile } from "../bridge.ts";
import { createSerial } from "./serial.ts";

export interface ProfileIO {
  load: () => Promise<Profile[]>;
  save: (profiles: Profile[]) => Promise<Profile[]>;
  /** Okunan ya da kaydedilen liste. */
  onChange: (profiles: Profile[]) => void;
}

export interface ProfileStore {
  /** Diskten okur; okunmuşsa yeniden okumaz. */
  load: () => Promise<void>;
  add: (profile: Profile) => Promise<void>;
  remove: (target: string) => Promise<void>;
}

export function createProfileStore(io: ProfileIO): ProfileStore {
  const serial = createSerial();
  // null: henüz diskten okunmadı.
  let current: Profile[] | null = null;

  const apply = (next: Profile[]): Profile[] => {
    current = next;
    io.onChange(next);
    return next;
  };
  const loaded = async (): Promise<Profile[]> => current ?? apply(await io.load());

  return {
    load: () =>
      serial(async () => {
        await loaded();
      }),
    add: (profile) =>
      serial(async () => {
        const list = await loaded();
        if (list.some((p) => p.target === profile.target)) return;
        apply(await io.save([...list, profile]));
      }),
    remove: (target) =>
      serial(async () => {
        const list = await loaded();
        if (!list.some((p) => p.target === target)) return;
        apply(await io.save(list.filter((p) => p.target !== target)));
      }),
  };
}

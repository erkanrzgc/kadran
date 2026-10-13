/**
 * Aynı anahtar (sunucu) için aynı anda tek iş: süren varsa yeni çağıran
 * onun sonucunu bekler. Aynı sunucuya iki istek üst üste binmediği için
 * geç gelen ESKİ cevap yenisinin üstüne yazamaz: eski başarı yeni hatanın
 * "bayat" işaretini silemez, eski VALID yeni INVALID'i gizleyemez.
 *
 * Takılan iş sonsuza dek kilitlemez: sidecar istemcisi her isteği
 * süreyle sınırlar (shared/sidecar-client.ts), iş o zaman reddedilir ve
 * kayıt silinir.
 */

export type SingleFlight<V> = (key: string, run: () => Promise<V>) => Promise<V>;

export function createSingleFlight<V>(): SingleFlight<V> {
  const running = new Map<string, Promise<V>>();
  return (key, run) => {
    const existing = running.get(key);
    if (existing) return existing;
    const job: Promise<V> = run().finally(() => {
      if (running.get(key) === job) running.delete(key);
    });
    running.set(key, job);
    return job;
  };
}

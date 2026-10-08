/**
 * İşleri çağrı sırasıyla, tek tek çalıştırır: bir iş bitmeden (başarıyla ya
 * da hatayla) sonrakisi başlamaz. Profil listesini yazan işler için gerekli:
 * ikisi üst üste binerse her biri aynı eski listeden başlar ve geç biten
 * öbürünün değişikliğini geri getirir ya da siler.
 *
 * Reddedilen iş yalnız kendi çağıranını reddeder; kuyruğu tıkamaz.
 */

export type Serial = <T>(job: () => Promise<T>) => Promise<T>;

export function createSerial(): Serial {
  let tail: Promise<unknown> = Promise.resolve();
  return <T>(job: () => Promise<T>): Promise<T> => {
    // Önceki işin sonucu ne olursa olsun sıra bize gelir.
    const result = tail.then(job, job);
    tail = result.catch(() => undefined);
    return result;
  };
}

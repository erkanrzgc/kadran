// Nabız kararı — saf fonksiyon, ağ ve depolama YOK (K-109).
//
// Worker'ın zamanlanmış kontrolü bunu çağırır: son nabzın zamanına ve
// önceki duruma bakıp NE yapılacağına karar verir. Kararı ayrı tutmak,
// mantığı Cloudflare'e yüklemeden `node --test` ile sınamayı sağlıyor.
//
// ── Neden alarm NABIZ GELMEDİĞİNDE çalıyor ──────────────────────────
//
// Alarmı nabız isteğinin içinden atmak, tam da yakalanmak istenen
// arızada susardı: ölü bir sunucu istek atmaz. Karar bu yüzden
// zamanlanmış kontrolde, YOKLUĞA bakarak veriliyor.
//
// Durumlar:
//   "var"  nabız taze
//   "yok"  nabız eşikten eski — alarm verildi
//   "hic"  hiç nabız gelmedi — kurulum eksik, bir kez söylendi
//
// Eylemler yalnızca GEÇİŞTE üretiliyor (kenar tetikleme): 15 dakika
// boyunca her 5 dakikada bir "hâlâ yok" mesajı, bakılmayan alarma
// dönüşürdü.

export const DURUM_VAR = "var";
export const DURUM_YOK = "yok";
export const DURUM_HIC = "hic";

/**
 * @param {number} simdi       ms
 * @param {number|null} son    son nabız, ms; hiç yoksa null
 * @param {string|null} durum  önceki durum; hiç yoksa null
 * @param {number} esikMs      bu kadar sessizlik = nabız yok
 * @returns {{eylem: "yok"|"alarm_yok"|"alarm_hic"|"duzeldi", yeniDurum: string|null}}
 *          yeniDurum null ise durum DEĞİŞMİYOR (yazma yok)
 */
export function karar(simdi, son, durum, esikMs) {
  if (son === null || !Number.isFinite(son) || son <= 0) {
    // Hiç nabız yok. Sessiz kalmak, yanlış kurulmuş bir nabzı
    // "her şey yolunda" gösterirdi. Bir kez söyle.
    if (durum === DURUM_HIC) return { eylem: "yok", yeniDurum: null };
    return { eylem: "alarm_hic", yeniDurum: DURUM_HIC };
  }

  const gecen = simdi - son;
  if (gecen > esikMs) {
    if (durum === DURUM_YOK) return { eylem: "yok", yeniDurum: null };
    return { eylem: "alarm_yok", yeniDurum: DURUM_YOK };
  }

  // Nabız taze.
  if (durum === DURUM_YOK || durum === DURUM_HIC) {
    return { eylem: "duzeldi", yeniDurum: DURUM_VAR };
  }
  if (durum !== DURUM_VAR) {
    // İlk sağlıklı gözlem: durumu kaydet ama mesaj atma.
    return { eylem: "yok", yeniDurum: DURUM_VAR };
  }
  return { eylem: "yok", yeniDurum: null };
}

/**
 * @param {"alarm_yok"|"alarm_hic"|"duzeldi"} eylem
 * @param {string} sunucu
 * @param {number} gecenDk  son nabızdan beri geçen dakika (bilinmiyorsa null)
 */
export function mesaj(eylem, sunucu, gecenDk) {
  switch (eylem) {
    case "alarm_yok":
      return `🔴 NABIZ YOK — ${sunucu}\n` +
        `${gecenDk} dakikadır haber alınamıyor.\n` +
        `Sunucu kapalı olabilir ya da alarm göndericisi durmuş olabilir; ` +
        `bu süre boyunca başka hiçbir alarm sana ulaşmaz.`;
    case "alarm_hic":
      return `🟡 NABIZ HİÇ GELMEDİ — ${sunucu}\n` +
        `Worker çalışıyor ama sunucudan tek bir nabız almadı. ` +
        `Kurulum tamamlanmamış olabilir (HEARTBEAT_URL / PING_TOKEN).`;
    case "duzeldi":
      return `✅ NABIZ GERİ GELDİ — ${sunucu}`;
    default:
      throw new Error(`bilinmeyen eylem: ${eylem}`);
  }
}

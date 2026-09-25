// Panely DIŞ nabız kontrolü — Cloudflare Worker (K-109).
//
// ── Neden sunucunun DIŞINDA ──────────────────────────────────────────
//
// Alarm göndericisi (K-108) sunucuda koşuyor. Kendisi durursa ya da
// sunucu tamamen kapanırsa, bunu bildirecek bir şey sunucuda kalmıyor.
// Bu Worker sunucudan dakikalarca haber alamazsa kendisi yazıyor.
//
// İki yol:
//   POST /ping   sunucudaki gönderici her ~5 dk'da bir çağırır.
//                YALNIZCA zamanı yazar, asla alarm vermez.
//   scheduled    5 dakikada bir. Son nabız eşikten eskiyse alarm verir.
//
// Alarm ping yolunda DEĞİL, zamanlanmış kontrolde: ölü sunucu ping
// atmaz, ping yolunda verilen bir alarm tam da aranan durumda susardı.
//
// Gizli değerler (wrangler secret put):
//   PING_TOKEN        sunucuyla paylaşılan rastgele dize
//   TELEGRAM_TOKEN    bot anahtarı
//   TELEGRAM_CHAT_ID  sohbet kimliği
import { karar, mesaj } from "./karar.js";

const KV_SON = "son_nabiz";
const KV_DURUM = "durum";

export default {
  async fetch(istek, env) {
    const url = new URL(istek.url);
    if (istek.method !== "POST" || url.pathname !== "/ping") {
      return new Response("bulunamadı\n", { status: 404 });
    }
    if (!env.PING_TOKEN) {
      // Yapılandırılmamış bir Worker her ping'i kabul etmemeli.
      return new Response("yapılandırılmamış\n", { status: 503 });
    }
    const gelen = istek.headers.get("Authorization") ?? "";
    if (!(await sabitZamanEsit(gelen, `Bearer ${env.PING_TOKEN}`))) {
      return new Response("yetkisiz\n", { status: 401 });
    }
    await env.NABIZ.put(KV_SON, String(Date.now()));
    return new Response(null, { status: 204 });
  },

  async scheduled(_olay, env) {
    const hamSon = await env.NABIZ.get(KV_SON);
    const son = hamSon === null ? null : Number(hamSon);
    const durum = await env.NABIZ.get(KV_DURUM);
    const esikMs = Number(env.ESIK_DAKIKA) * 60_000;
    if (!Number.isFinite(esikMs) || esikMs <= 0) {
      throw new Error(`ESIK_DAKIKA geçersiz: ${env.ESIK_DAKIKA}`);
    }

    const simdi = Date.now();
    const { eylem, yeniDurum } = karar(simdi, son, durum, esikMs);

    if (eylem !== "yok") {
      const gecenDk = son ? Math.round((simdi - son) / 60_000) : null;
      // Gönderim başarısızsa durum YAZILMIYOR: bir sonraki kontrol
      // (5 dk sonra) aynı alarmı yeniden dener. Durumu önce yazıp
      // sonra göndermek, Telegram'ın kısa bir kesintisinde alarmı
      // sonsuza dek yutardı.
      await telegram(env, mesaj(eylem, env.SUNUCU ?? "sunucu", gecenDk));
    }
    if (yeniDurum !== null) {
      await env.NABIZ.put(KV_DURUM, yeniDurum);
    }
  },
};

async function telegram(env, metin) {
  const yanit = await fetch(
    `https://api.telegram.org/bot${env.TELEGRAM_TOKEN}/sendMessage`,
    {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ chat_id: env.TELEGRAM_CHAT_ID, text: metin }),
    },
  );
  if (!yanit.ok) {
    // Anahtar URL'de; hata iletisine URL KONMUYOR.
    throw new Error(`Telegram gönderimi başarısız: http ${yanit.status}`);
  }
}

// Sabit zamanlı karşılaştırma. Uzunlukları farklı iki dizeyi doğrudan
// karşılaştırmak uzunluğu sızdırır; önce ikisinin de SHA-256'sı alınır,
// sonra eşit uzunluktaki özetler karşılaştırılır.
async function sabitZamanEsit(a, b) {
  const kodla = new TextEncoder();
  const [ha, hb] = await Promise.all([
    crypto.subtle.digest("SHA-256", kodla.encode(a)),
    crypto.subtle.digest("SHA-256", kodla.encode(b)),
  ]);
  return crypto.subtle.timingSafeEqual(ha, hb);
}

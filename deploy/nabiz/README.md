# Dış nabız kontrolü (Cloudflare Worker)

Alarm göndericisi (`deploy/notify`) sunucuda koşuyor. Kendisi durursa ya da
sunucu tamamen kapanırsa bunu bildirecek bir şey sunucuda kalmaz. Bu Worker
sunucudan 15 dakika haber alamazsa Telegram'a yazar. Ayrıntı: K-109.

```
sunucu  panely-notify (dakikada bir)  ──POST /ping, ~5 dk'da bir──►  Worker ─► KV
Worker  5 dk'da bir kontrol: son nabız > 15 dk?  ──►  Telegram
```

Ücretsiz katman: günde ~320 KV yazması (ölçüldü; sınır 1000), 1 zamanlanmış
tetikleyici (sınır 5).

## Kurulum

Gereken: Node.js ve Cloudflare hesabı. Komutlar bu dizinde çalıştırılır.

### 1. Giriş ve yükleme (kendi bilgisayarında)

```bash
cd deploy/nabiz
npx wrangler login
npx wrangler deploy
```

`deploy`, KV ad alanını kendisi oluşturur ve kimliğini `wrangler.jsonc`'a
yazar. **Bu değişikliği commit etme** — kimlik senin hesabına özel.
Çıktıdaki adresi not al: `https://panely-nabiz.<alt-alan>.workers.dev`.

### 2. Gizli değerler

Nabız anahtarını üret ve bir yere kopyala:

```bash
openssl rand -hex 32
```

Sonra üç değeri Worker'a gir (her biri seni değeri yapıştırmaya çağırır):

```bash
npx wrangler secret put PING_TOKEN          # az önce üretilen dize
npx wrangler secret put TELEGRAM_TOKEN      # bot anahtarı
npx wrangler secret put TELEGRAM_CHAT_ID    # sohbet kimliği
```

### 3. Sunucuya nabız ayarı (root)

`/etc/panely/notify.conf`'a iki satır ekle:

```ini
HEARTBEAT_URL=https://panely-nabiz.<alt-alan>.workers.dev/ping
HEARTBEAT_TOKEN=<aynı PING_TOKEN>
```

Bir dakika içinde ilk nabız gider.

## Doğrulama

Kontrol grubu şart: yalnızca "alarm geldi" görmek, Worker'ın ayırt
edebildiğini kanıtlamaz.

1. **Sessizlik:** gönderici çalışırken en az bir kontrol turu (5 dk)
   boyunca mesaj GELMEMELİ.
2. **Alarm:** `systemctl stop panely-notify.timer` → en geç ~20 dk içinde
   "🔴 NABIZ YOK" gelmeli.
3. **Düzelme:** `systemctl start panely-notify.timer` → "✅ NABIZ GERİ GELDİ".

Ölçülen (25 Eyl, K-109): son nabızdan 17 dk sonra 🔴, zamanlayıcı açıldıktan
sonraki ilk turda ✅. Her durum değişiminde tek mesaj geldi, eşiğin altındaki
turlar sustu.

## Bilinen sınırlar

- Nabız, alarm göndericisi BAŞARIYLA koştuğunda atılır. Telegram'a
  ulaşılamadığı için gönderici başarısız olursa nabız da kesilir ve Worker
  alarm verir — Worker da Telegram'a ulaşamıyorsa o mesaj da gitmez.
- Worker'ın kendisi durursa (Cloudflare kesintisi, hesap sorunu) kimse haber
  almaz. Zincirin son halkası.

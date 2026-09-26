# Alarm teslimatı (Telegram)

panelyd alarmları tespit ediyor ama dışarı gönderemiyor: `IPAddressDeny=any`
taşıyor, kasıtlı. Bu birim alarmları panelyd'nin journal'ından okuyup
Telegram'a iletir. Çekirdek servisler (panelyd, panely-exec, panely-caddy)
çökerse ya da durursa onu da bildirir. Ayrıntı ve ölçümler:
`docs/decisions.md`, K-108 ve K-110.

## Ne gönderilir

| olay | kaynak |
|---|---|
| 🔴 KRİTİK / 🟡 UYARI açıldı | panelyd `msg=ALARM durum=acildi` |
| 🔴 KÖTÜLEŞTİ | `durum=tirmandi` |
| ✅ DÜZELDİ | `durum=kapandi` |
| 🔴 BİRİM BAŞARISIZ | `OnFailure=` — şu an `panely-offsite.service` |
| 🟠 ÇÖKTÜ | çekirdek servis çöktü (systemd'nin "Failed with result" olayı) |
| 🔴 ÇÖKME DÖNGÜSÜ | bir sonraki kontrolde de çökmüş; döngü sürdükçe yeni mesaj YOK |
| 🔴 ÇALIŞMIYOR | iki kontrol üst üste süreci yok (`exit 0`, `stop`, vazgeçildi) |
| ✅ TOPARLANDI / YENİDEN ÇALIŞIYOR | çöküş ya da duruş bitti |

Gecikme en fazla ~1 dakika. Gönderim başarısız olursa olay KAYBOLMAZ;
bir sonraki koşuda yeniden denenir.

Servis mesajları yalnızca systemd'nin alanlarını taşır (sonuç: `signal`,
`exit-code`, `oom-kill`…). Servisin kendi çıktısı, ör. bir panik mesajı,
Telegram'a gönderilmez.

## Güvenlik modeli

- Gönderici `panely` DEĞİL, her koşuda geçici bir kullanıcı (`DynamicUser`).
- Anahtar dosyası `0600 root:root`. systemd onu root olarak okuyup yalnızca
  bu birime verir (`LoadCredential`). **panelyd anahtarı okuyamaz**; ele
  geçirilse bile sizin adınıza mesaj atamaz.
- Ağ: yalnızca dışarı. Yerel ağ kapalı, tek istisna DNS çözücüsü (K-107).
- ⚠ Gönderici bütün sistem journal'ını okuyabilir (`systemd-journal`
  grubu). Bilinçli bir denge; gerekçe birim dosyasında.

## Kurulum

### 1. Bot oluştur (Telegram'da)

1. Telegram'da **@BotFather**'a yaz → `/newbot` → ad ver.
2. Verdiği anahtarı (`123456789:ABC…`) kopyala. **Kimseyle paylaşma.**
3. Yeni botuna Telegram'dan herhangi bir mesaj yaz (ör. `merhaba`).
   Bot, sana ancak sen ona önce yazdıysan mesaj atabilir.

### 2. Anahtarı sunucuya yaz (root)

```bash
sudo install -m 0600 -o root -g root /dev/null /etc/panely/notify.conf
sudo nano /etc/panely/notify.conf
```

```ini
TELEGRAM_TOKEN=123456789:ABC…
TELEGRAM_CHAT_ID=
```

### 3. Sohbet kimliğini bul

```bash
sudo /usr/local/lib/panely/notify/panely-notify.sh sohbet-bul
```

Çıkan `sohbet kimliği: 123456789` sayısını `TELEGRAM_CHAT_ID=` satırına yaz.

### 4. Dene — gerçek birimle

```bash
sudo systemctl start panely-notify-failure@deneme.service
```

Telegram'a "BİRİM BAŞARISIZ — deneme" mesajı gelmeli. Bu, gerçek birimin
kısıtlarıyla (geçici kullanıcı, kimlik bilgisi, ağ) uçtan uca denemedir.

### 5. Aç

```bash
sudo systemctl enable --now panely-notify.timer
```

İlk koşu imleci "şimdi"ye koyar; kurulumdan önceki alarmlar gönderilmez.

## Bilinen sınırlar

- **Göndericinin kendisi düşerse** sunucuda bunu yakalayacak bir şey yok.
  Dış nabız kontrolü ([`deploy/nabiz`](../nabiz/README.md), K-109) 15
  dakika nabız alamazsa haber verir.
- **Askıda kalma yakalanmaz.** Çekirdek birimlerde `WatchdogSec` yok;
  kilitlenen bir panelyd'nin süreci durur ama yaşar, "çalışıyor" görünür.
- Bakım için bir çekirdek servisi iki dakikadan uzun durdurmak da
  "🔴 ÇALIŞMIYOR" mesajı üretir. Bu doğru: servis gerçekten çalışmıyor.
- Bu kurulum `panely bootstrap` tarafından yapılmıyor (uzak yedek gibi).

# TOTP: iki aşamalı giriş ve kısa ömürlü oturum jetonu

**Tarih:** 24 Ekim 2026
**Durum:** TASARIM. Kullanıcı soru istemedi ("başla"). Aşağıdaki her şey benim kararım,
**(karar)** diye işaretli ve K-155'e yazılacak. Kullanıcıdan gelen tek şey sıra ve kapsam: "TOTP ve
rate limiting, en mantıklı sırayla". Önce TOTP: şartname §1.3'ü ve K-076'nın beklettiği kapıyı
açıyor. Caddy'de rate limiting ayrı bir proje.
**İlgili:** şartname §1.3; K-001 (web paneli yok), K-076 (yıkıcı işlemin TOTP kapısı), K-131
(roller), K-141 (İngilizce), K-143 (aracı).

## Amaç

Ele geçirilmiş bir yönetici SSH anahtarı tek başına sunucuyu yönetemesin. Bugün `kadran-client`'a
giren bir yönetici anahtarı her şeyi yapıyor: ortam değişkenlerinin ve derleme argümanlarının
DEĞERLERİNİ okuyor (`GetApp`), uygulama siliyor, geri alıyor, yedek alıyor. Şartname §1.3 "tüm giriş
süreçlerinde TOTP zorunlu" ve "imzalanmış kısa ömürlü jetonlar" diyor. Kadran'da giriş ekranı yok;
her CLI ya da masaüstü çağrısı ayrı bir SSH bağlantısı. Bu yüzden "giriş" bir kodla alınan, birkaç
saat geçerli bir jeton oluyor **(karar)**.

Yalnız yıkıcı işlemlere kod sormak yetmezdi: `GetApp` sırları okuyor, yani çalınmış anahtar kod
sorulmayan bir okuma ile zaten en değerli şeyi alırdı.

Başarı ölçütleri:

1. TOTP açık bir sunucuda yönetici çağrısı (ör. `kadran status`) jeton olmadan reddedilir:
   `TOTP is on for this server: run kadran login <target>`.
2. `kadran login <target>` kodu sorar. Doğru kodla 8 saatlik jeton alınır ve sonraki çağrılar soru
   sormaz.
3. Jeton başka bir anahtarın bağlantısında, süresi dolmuşken, kurcalanmışken ya da TOTP yeniden
   kurulmadan önce basılmışsa reddedilir.
4. Dağıtım anahtarı (CI, K-131) etkilenmez.
5. 5 ardışık yanlış koddan sonra o anahtar artan sürelerle kilitlenir. Her başarısızlık denetim
   kaydına girer, kilit alarm (Telegram) üretir.
6. Telefon kaybolursa kurtarma koduyla girilir. O da yoksa sunucuda root (sudo SSH)
   `kadran totp reset` ile TOTP'yi kapatır.
7. TOTP açılana kadar hiçbir davranış değişmez: yükseltme kimseyi kilitlemez.

## Kapsam dışı

- **Yıkıcı işlemlerde ayrıca taze kod (step-up) ve K-076'nın gevşetilmesi** (canlı uygulamayı
  silmek): ayrı karar. Tek istisna `DisableTotp` (aşağıda).
- **Masaüstünde kod penceresi.** v1'de masaüstünün sidecar'ı CLI ile aynı ikili ve aynı jeton
  dosyasını okuyor; jeton yoksa ya da süresi dolduysa "run kadran login" diyen hata gösterir
  **(karar)**.
- **QR kod.** Bağımlılık gerektirir. `otpauth://` URI'si ve base32 sır basılır; bütün
  doğrulayıcı uygulamalar elle girişi kabul ediyor **(karar)**.
- **Anahtar başına ayrı TOTP.** Tek operatör, sunucu başına tek sır **(karar)**. Jeton yine de
  giriş yapan anahtara bağlı.
- **Sunucuda tek tek jeton iptali.** `logout` yalnız istemcideki dosyayı siler. Hepsini iptal
  etmek için TOTP yeniden kurulur (anahtar döner).
- **Uygulama trafiğinde rate limiting** (Caddy): ayrı proje.

## Tasarım

### Algoritma

RFC 6238: HMAC-SHA1, 30 saniyelik adım, 6 hane, ±1 adım tolerans **(karar)**. Standart
kütüphaneyle (`crypto/hmac`, `crypto/sha1`); RFC 6238 Ek B'nin test vektörleriyle sınanır. Yeni
bağımlılık yok, `go.mod` değişmez.

### Durum (SQLite, göç `0011_totp`)

- `totp` (tek satır): `secret` (20 bayt, etkin sır; yoksa NULL), `confirmed_at`, `last_step` (son
  kabul edilen adım), `token_key` (32 bayt), `generation`, `pending_secret` ve
  `pending_created_at` (onay bekleyen kurulum; yeniden kurulumda etkin sırrın yanında durur).
  TOTP'nin "açık" olması = `secret` dolu.
- `totp_recovery`: `hash` (SHA-256), `used_at`.
- `totp_failures`: `fingerprint`, `count`, `locked_until`, `last_failure_at`.

Sır `kadran.db`'de düz duruyor **(karar)**. Kasa (K-123) kullanılamaz: kadrand değerleri
mühürleyebilir ama AÇAMAZ, özel anahtar executor'da. Doğrulamayı executor'a taşımak da olmaz:
ayrıcalıklı yüzey bütçesi dolmak üzere ve TOTP ayrıcalık gerektirmiyor. Sonuç: `kadran.db` ve
yedekleri sırrı taşıyor. Yerel yedekler `0600 kadran`, uzak yedek age ile şifreli (K-098). README'nin
güvenlik bölümüne yazılır.

Durum her çağrıda veritabanından okunur, bellekte tutulmaz **(karar)**: `reset` ve `disable` anında
etki eder, kadrand'ı yeniden başlatmak gerekmez.

### Zorlama

`authorize` (K-131'in önleyicisi) içinde:

- Rol `deploy` → değişmez.
- Rol `admin` ve TOTP açık → yöntem `Ping`, `Login` dışındaysa `kadran-session` metadata'sında
  geçerli jeton zorunlu. `Ping` serbest kalıyor çünkü istemci bağlanırken protokolü onunla soruyor.
- TOTP kapalı (yalnız onay bekleyen kurulum olsa bile) → değişmez.

Jeton biçimi: `v1.<base64url(payload)>.<base64url(HMAC-SHA256(token_key, payload))>`, payload JSON
`{"fp": "<parmak izi>", "exp": <unix>, "gen": <generation>}`. Doğrulama sırası: biçim → HMAC (sabit
zamanlı karşılaştırma) → `fp` önsözdeki parmak izine eşit → `exp` geçmemiş → `gen` güncel. Metadata'yı
uzak istemci yazıyor (bkz. `identity.go`); jeton bu yüzden bir taşıyıcı kimlik bilgisi ve ancak
bağlantının kendi parmak iziyle birlikte geçerli.

Parmak izi boşsa (yerel çağıran, `ExposeAuthInfo` yoksa) jeton basılmaz ve kabul edilmez. TOTP
açıkken parmak izsiz yönetici çağrısı yapılamaz **(karar)**. Root'un kaçış yolu ayrı (aşağıda).

Süre 8 saat **(karar)**, bir iş günü.

### RPC'ler

Protokol 3 kalıyor **(karar)**: yalnız yeni yöntemler ve bir `optional` alan ekleniyor.

- `Login(code | recovery_code) → session_token, expires_at`
- `BeginTotpEnrollment() → otpauth_uri, secret_base32`: TOTP kapalıysa yönetici çağırır; açıksa
  geçerli jeton gerekir (yeniden kurulum, yeni sır `pending` olarak yanında bekler, onaylanana kadar
  eskisi geçerli).
- `ConfirmTotpEnrollment(code) → recovery_codes[10], session_token, expires_at`: kod BEKLEYEN
  sırla doğrulanır; `pending_secret` etkin sır olur, `generation` artar, `token_key` yenilenir (eski
  jetonların hepsi düşer), kurtarma kodları yenilenir. Kurtarma kodları YALNIZ burada, bir kez
  döner. Onaylanmamış kurulum 10 dakika sonra geçersiz **(karar)**.
- `DisableTotp(code) → {}`: jetonun yanında TAZE kod ister **(karar)**. Çalınmış bir jeton TOTP'yi
  kapatamasın.
- `GetTotpStatus() → state, confirmed_at, recovery_remaining, locked fingerprints`
- `PingResponse.totp_required` (`optional bool`): istemci reddi beklemeden bilir.

Uyum:

- Eski istemci + TOTP açık sunucu: `Unauthenticated`, metni
  `TOTP is on for this server: upgrade kadran and run kadran login`.
- Yeni istemci + eski sunucu: `Login` `Unimplemented` → `this server does not support TOTP;
  upgrade it`.

### Kaba kuvvet ve tekrar

- Parmak izi başına ardışık başarısızlık sayacı, KALICI (yeniden başlatma sıfırlamaz).
- 5. başarısızlıktan sonra kilit: 1 dakika, sonraki her başarısızlıkta ikiye katlanır, en çok
  1 saat **(karar)**. Kilitliyken kod hiç denenmez; doğru kod da reddedilir ve cevap kalan süreyi
  söyler.
- Başarılı giriş sayacı sıfırlar.
- Her başarısızlık `auth.login` denetim kaydı (sebep: yanlış kod, kilitli, tekrar). Kilit açılınca
  `login_lockout` alarmı (uyarı) → Telegram (alarm türü göndericiye eklenir).
- Kurtarma kodu denemeleri aynı sayaca girer.
- Tekrar: kabul edilen adım `last_step`'e yazılır; `last_step` ve öncesindeki adımlar reddedilir.
  Sunucu başına tek sır olduğundan sayaç da tek.

Kaba kuvvetin sınırı: kilit olmadan 6 hane ve ±1 adım, deneme başına ~3/10⁶. Kilitle bir anahtar
saatte en çok ~10 deneme yapabilir.

### Kurtarma

- 10 kod, her biri 10 karakter base32 (~50 bit), SHA-256 ile saklanır **(karar)**. Rastgele ve
  yüksek entropili oldukları için yavaş özet gerekmez. Tek kullanımlık.
- Kullanım denetime ve uyarı alarmına yazılır. Kalan sayı `kadran totp status`'ta görünür.
- **Son çare:** `kadran totp reset -sudo <kurulum-kullanıcısı>@<host>`, `bootstrap`'in kullandığı
  kurulum SSH'ı ile gider ve sunucuda kadran kullanıcısı olarak (`setpriv`) `kadrand -totp-reset`
  çalıştırır. TOTP satırı, kurtarma kodları ve sayaçlar silinir; denetim kaydı yazılır
  (`origin=local`). kadrand durmaz: denetim eklemesi `BEGIN IMMEDIATE` ile süreçler arası güvenli
  (`store.go`) ve durum her çağrıda okunuyor. Bu yol TOTP açılmadan ÖNCE yazılır ve sınanır.

### İstemci

- `kadran login [-recovery] <target>`: kodu standart girdiden okur. Kod argümana yazılmaz, kabuk
  geçmişine düşmesin.
- `kadran logout <target>`: jeton dosyasını siler.
- `kadran totp enable | disable | status <target>`, `kadran totp reset -sudo <target>`.
- Jeton dosyası: `os.UserConfigDir()/kadran/sessions/<sha256(target)[:16]>.json`, `0600`; içinde
  hedef, jeton, bitiş. Her RPC'ye (tekli ve akış) önleyiciyle eklenir; süresi dolmuşsa eklenmez.
- Masaüstü sidecar'ı aynı ikili, aynı kullanıcı dizini: aynı dosyayı okur.

### Denetim

`totp.enroll`, `totp.confirm`, `totp.disable`, `totp.reset`, `auth.login` (başarılı ve başarısız).
Sır, kod, kurtarma kodu ve jeton hiçbir kayda, günlüğe ya da alarm metnine YAZILMAZ; yalnız parmak
izi, sonuç ve sebep yazılır.

## Değişmezler

- Ayrıcalıklı yüzey değişmez: executor'a dokunulmuyor.
- `go.mod` ve `go.sum` değişmez.
- İngilizce koruması (K-141, K-153) yeni metinlerden geçer.
- Yeni davranış testli, yeni mutasyon betiği (`mutate-totp.sh`) üç kapıyla (derleme, tek eşleşme,
  yeşil taban).

## Canlı ölçüm planı

Her adım ayrı onayla. Önce GCP: enable → login → status → başka bir anahtarla aynı jeton → yanlış
kodla kilit → kurtarma kodu → `reset -sudo`. Sonra Hetzner, ancak kullanıcı isterse açık kalır.

## Geliştiriciye kalanlar

Paket ve dosya adları, hata metinlerinin tam ifadesi, tabloların sütun tipleri, istemci tarafında
kodun nasıl istendiği (TTY'de satır okuma). Bunlar tasarımı değiştirmeden geliştirmede seçilir.

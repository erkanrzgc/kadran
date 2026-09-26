# Uzak yedek kurulumu

Yerel yedekler (`/var/lib/panely/backups`) diskin kendisi giderse
kaybolur. Bu birim onları **şifreleyip** bir uzak hedefe kopyalar.

## Tehdit modeli — ne koruyor, ne korumuyor

| durum | sonuç |
|---|---|
| Disk ölür / sunucu silinir | ✅ uzak kopyadan geri yüklenir |
| Sunucu ele geçirilir, saldırgan **geçmiş yedekleri okumak** ister | ✅ çözemez — özel anahtar burada yok |
| Sunucu ele geçirilir, saldırgan **uzak yedekleri silmek** ister | ⚠ sağlayıcı tarafında kısıtlanmazsa SİLEBİLİR |
| Özel anahtar kaybolur | ❌ yedekler KURTARILAMAZ |

Son iki satır gerçek ve hafifletilebilir; aşağıda nasıl olduğu yazıyor.

## Neden şifreleme zorunlu

Yedekler sır taşıyor. Varsayılmadı, ölçüldü:

```
sqlite3 yedek.db "SELECT env_json FROM apps"
→ {"DATABASE_URL":"postgres://panely:<parola>@db:5432/..."}
```

Şifresiz yükleme, uygulama sırlarını üçüncü tarafa vermek olurdu.

## Kurulum

### 1. Anahtar çiftini KENDİ makinende üret

Özel anahtar **sunucuya asla girmez.**

```bash
age-keygen -o panely-yedek-anahtari.txt
```

Çıktının içinde bir `# public key: age1...` satırı var. **Açık
anahtar** sunucuya gider, dosyanın tamamı sende kalır.

> ⚠ `panely-yedek-anahtari.txt` dosyasını en az iki ayrı yerde sakla
> (parola yöneticisi + çevrimdışı kopya). Kaybolursa yedekler
> çözülemez. Sunucuda saklama — orada durması bütün amacı bozar.

### 2. Uzak hedefi tanımla (sunucuda, root olarak)

```bash
sudo install -d -m 0755 -o root -g root /etc/panely
sudo rclone config --config /etc/panely/rclone.conf
sudo chown root:panely /etc/panely/rclone.conf
sudo chmod 0640 /etc/panely/rclone.conf
```

Yol SABİT: birim `RCLONE_CONFIG=/etc/panely/rclone.conf` ile başlıyor.

> ⚠ **Yapılandırmayı `/var/lib/panely` altına KOYMA** (rclone'un
> `panely` kullanıcısı için varsayılan yeri orası). O dizin daemon'un;
> ele geçirilen bir panelyd oradaki dosyayı silip yerine kendisininkini
> koyabilir — dosyanın sahibi root olsa bile. rclone yapılandırması
> komut çalıştırabildiği için (ör. webdav `bearer_token_command`), bu
> ağı olmayan daemon'a ağ gören bir süreçte komut çalıştırma yolu açardı.
> `/etc/panely` root'un dizini; `panely` orada dosya silemez. Bkz. K-100.

Hedefi `panely-offsite` diye adlandır. Backblaze B2 ve S3 uyumlu her
sağlayıcı çalışır.

**Sağlayıcıda silme yetkisi VERME.** Bu, "ele geçirilen sunucu uzak
yedekleri silebilir" satırını kapatan tek şey:

- **Backblaze B2:** uygulama anahtarını `listBuckets, listFiles,
  readFiles, writeFiles` ile oluştur — `deleteFiles` VERME. Kovada
  Object Lock / sürümleme aç.
- **S3:** IAM politikasında `s3:DeleteObject` reddedilsin, kovada
  versioning + MFA delete açık olsun.
- **Cloudflare R2:** aşağıdaki ayrı bölüme bak — R2'de "yaz ama silme"
  izni YOK, koruma başka yoldan kuruluyor.

Silme yetkisi vermezsen betiğin uzak budaması çalışmaz; bu bir
kusur değil, tercih. `offsite.conf`'a `OFFSITE_PRUNE=hayir` yaz ve
eskiyenleri sağlayıcının yaşam döngüsü kuralına bırak.

#### Cloudflare R2 (ücretsiz katman: 10 GB, çıkış ücreti yok)

Ölçülen boyut (21 Eyl): şifreli bir yedek 143.592 bayt. Günde 24
yedek × 90 gün ≈ 310 MB — ücretsiz katmanın çok altında. Uygulama
eklendikçe veritabanı büyür; oran değişirse yeniden ölç.

⚠ **R2 token'larında silmesiz yazma izni YOK.** Seçenekler Admin
Read & Write, Admin Read, Object Read & Write, Object Read. Yazabilen
her token SİLEBİLİR. Silmeyi durduran şey kovadaki **bucket lock**:

1. Kova oluştur: `panely-yedek` (Standard sınıf — ücretsiz katman
   Infrequent Access'e UYGULANMIYOR).
2. **Bucket lock** kuralı ekle: önek `panely-`, saklama **30 gün**.
   Kilitli bir nesne o süre dolmadan silinemez ve üzerine yazılamaz.
3. **Yaşam döngüsü** kuralı ekle: önek `panely-`, **90 gün** sonra sil.
   Kilitten uzun olmalı; kilit her zaman önceliklidir.
4. API token: **Object Read & Write**, YALNIZCA `panely-yedek` kovasına.
   Admin token KULLANMA: kova yönetimi yetkisi taşır ve kilit bir kova
   ayarıdır — sunucudaki bir anahtarın kilidi değiştirebilmesi,
   kilidin amacını boşa çıkarırdı.
5. Sunucuda `rclone config --config /etc/panely/rclone.conf` ile
   `panely-offsite` adında bir `s3` hedefi kur. Sonuç şöyle görünmeli:

   ```ini
   [panely-offsite]
   type = s3
   provider = Cloudflare
   access_key_id = …
   secret_access_key = …
   endpoint = https://<hesap-kimliği>.r2.cloudflarestorage.com
   acl = private
   no_check_bucket = true
   no_head = true
   ```

   `no_check_bucket = true` ŞART: nesne düzeyindeki token kova
   oluşturamaz ve rclone aksi hâlde "Access Denied" ile düşer
   (Cloudflare'in kendi belgesi).

   `no_head = true` de ŞART (24 Eyl'de ölçüldü, K-106): bucket lock
   açık kovada R2 her yüklemeye bir sürüm kimliği döndürüyor; rclone
   1.60 yüklemeden sonra `HEAD ?versionId=…` atıyor ve R2 buna
   `501 Not Implemented` veriyor. Dosya YAZILIYOR ama rclone çıkış 1
   dönüyor ve betik her yüklemeyi başarısız sayardı. Yükleme sonrası
   boyut doğrulamasını betik zaten kendisi yapıyor.
6. `offsite.conf`'a `OFFSITE_PRUNE=hayir` yaz. Budama kilitli
   dosyaları silmeye çalışıp her koşuda hata basardı.

**Kilit ölçülmeden güvenilmez.** Cloudflare belgesi kilidin token
iznine ağır bastığını açıkça YAZMIYOR. Kurulumdan sonra aynı token'la
iki silme denenmeli:

- kilitli önekte (`panely-…`) bir dosya → **reddedilmeli**
- kilitsiz önekte bir sınama dosyası → **silinmeli** (kontrol grubu:
  token'ın silme yetkisi olduğunu, reddin kilitten geldiğini kanıtlar)

⚠ **Kilidin bedeli — maliyet.** Sunucu ele geçirilirse yazabilen
anahtar kovaya `panely-` önekli BÜYÜK dosyalar yükleyebilir. Ücretsiz
katman 10 GB; üstü ücretli, ve kilit bu dosyaların da 30 gün
silinmesini engeller. Kilit süresini gereğinden uzun tutma; 30 gün,
"fark et ve müdahale et" için yeterli bir pencere. Cloudflare
hesabındaki kullanım/fatura bildirimlerini kontrol et.

### 3. Yapılandırmayı yaz (sunucuda)

```bash
sudo tee /etc/panely/offsite.conf >/dev/null <<'CONF'
OFFSITE_REMOTE=panely-offsite:panely-yedek
OFFSITE_RECIPIENT=age1...            # 1. adımdaki AÇIK anahtar
OFFSITE_KEEP=30
# OFFSITE_PRUNE=hayir                # R2 / silmesiz token: budamayı kapat
CONF
sudo chmod 0640 /etc/panely/offsite.conf
sudo chgrp panely /etc/panely/offsite.conf
```

⚠ `rclone.conf` sağlayıcı anahtarını taşır ve yükleyici ile daemon aynı
kullanıcıyla (`panely`) koştuğu için daemon onu OKUYABİLİR — ama
DEĞİŞTİREMEZ. Bu yüzden 2. adımdaki silme yetkisi kısıtı zorunlu:
okunan anahtar yedekleri silemesin. OAuth tabanlı sağlayıcılar
(Google Drive, OneDrive) jetonu yenileyip dosyaya YAZMAK ister; salt
okunur dosyada bu başarısız olur. Anahtar tabanlı B2/S3 kullan.

### 4. Zamanlayıcıyı aç

```bash
sudo systemctl enable --now panely-offsite.timer
sudo systemctl start panely-offsite.service   # ilk koşuyu hemen yap
journalctl -u panely-offsite -n 30 --no-pager
```

## Geri yükleme

```bash
# 1. Uzaktan indir
rclone copy panely-offsite:panely-yedek/panely-20260918T083505Z.db.age .

# 2. KENDİ makinende çöz (özel anahtar burada)
age -d -i panely-yedek-anahtari.txt \
    -o panely.db panely-20260918T083505Z.db.age

# 3. Sunucuya taşı ve geri yükle
scp panely.db root@sunucu:/tmp/
ssh root@sunucu 'systemctl stop panelyd && \
  /usr/local/lib/panely/panelyd --restore /tmp/panely.db'
```

`--restore` çalışmadan önce mevcut veritabanının güvenlik kopyasını
alır ve geri yüklenen dosyanın bütünlüğünü doğrular (K-091).

> ⚠ **Bu yedekler hacim verisini taşımıyor.** Yalnızca kontrol
> düzlemi veritabanı: uygulama tanımları, sürümler, denetim zinciri.
> Konteynerlerin kalıcı diskleri (`/var/lib/panely/volumes`) için
> aşağıdaki **hacim yedeği** ayrıca kurulmalı.

## Hacim yedeği — uygulama verisi (K-111)

panelyd uygulamaların kalıcı disklerini okuyamıyor ve bu bir güvence
(K-091). Hacim verisini ayrı bir birim arşivliyor:

| | |
|---|---|
| okur | bütün hacimleri — tek yetki: `CAP_DAC_READ_SEARCH` |
| ulaşır | hiçbir yere — ağ yok, soket yok (Docker soketi dahil) |
| yazar | yalnızca `/var/lib/panely-volume-backup` |
| verir | `age` ile şifreli arşiv, uzak yedekle aynı açık anahtar |

panelyd arşivleri okuyabilir ama içlerini çözemez; silemez, üzerine
yazamaz. Yükleyici arşivleri olduğu gibi (yeniden şifrelemeden) uzağa
taşır. Hepsi sunucuda kontrol gruplu ölçüldü.

### ⚠ Anlık görüntü DEĞİL

Uygulama durdurulmuyor; dosyalar uygulama çalışırken tek tek okunur.
Veritabanı taşıyan bir uygulamada dosyalar farklı anlardan gelebilir ve
geri yüklenen kopya **bozuk** olabilir. Veritabanını hacme bir
**döküm** olarak da al — döküm dosyası tutarlıdır:

```bash
pg_dump -U app app > /data/dump.sql            # PostgreSQL
sqlite3 /data/app.db ".backup /data/yedek.db"  # SQLite
```

### Kurulum

Uzak yedek (yukarısı) kurulu olmalı: alıcı anahtar `offsite.conf`'tan
okunur.

```bash
sudo install -m 0755 deploy/offsite/panely-volume-backup.sh /usr/local/lib/panely/offsite/
sudo install -m 0644 deploy/systemd/panely-volume-backup.service \
                     deploy/systemd/panely-volume-backup.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now panely-volume-backup.timer
sudo systemctl start panely-volume-backup.service   # ilk koşuyu hemen yap
journalctl -u panely-volume-backup -n 20 --no-pager
```

`offsite.conf` ve üst dizinleri root'a ait olmalı, grup ve diğerleri
yazamamalı; değilse birim çalışmayı reddeder. Alıcıyı değiştirebilen
biri, bütün uygulama verisini kendi anahtarına şifreletirdi.

Birim her gece 23:30'da koşar, yükleyici gece yarısı alır. Yerelde
uygulama başına `OFFSITE_VOLUME_KEEP` (varsayılan 3) arşiv tutulur.
Uzak budama (`OFFSITE_PRUNE=evet`) hacim arşivlerini **uygulama başına**
`OFFSITE_KEEP` kadar tutar.

### Maliyet

Her gece her uygulamanın TAM arşivi alınır, artımlı değil. R2 kilidi
(30 gün) ve yaşam döngüsü (90 gün) ile her arşiv uzakta ~90 gün durur:
günde X MB → uzakta ~90 × X MB. R2'nin ücretsiz katmanı 10 GB; bütün
uygulamaların arşivi toplam günde ~110 MB'ı geçerse ücretli katmana
girersin.

## Doğrulama

Betik yüklemeyi **ölçüyor**, varsaymıyor: her dosyadan sonra uzaktaki
boyutu okuyup yereldekiyle karşılaştırıyor. Uyuşmazsa birim başarısız
oluyor.

Kısmi başarı başarı sayılmıyor — tek bir dosya bile yüklenemezse
`panely-offsite.service` `failed` durumuna geçer:

```bash
systemctl status panely-offsite.service
systemctl list-timers panely-offsite.timer
```

Arıza Telegram'a bildirilir: birim `OnFailure=` ile alarm göndericisini
çağırıyor (K-108, [`deploy/notify`](../notify/README.md)). Gönderici
kurulu değilse birim yine `failed` kalır ama kimse haber almaz.

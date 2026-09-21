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

Silme yetkisi vermezsen betiğin uzak budaması çalışmaz; bu bir
kusur değil, tercih. `OFFSITE_KEEP` yerine sağlayıcının yaşam döngüsü
kuralını kullan.

### 3. Yapılandırmayı yaz (sunucuda)

```bash
sudo tee /etc/panely/offsite.conf >/dev/null <<'CONF'
OFFSITE_REMOTE=panely-offsite:panely-yedek
OFFSITE_RECIPIENT=age1...            # 1. adımdaki AÇIK anahtar
OFFSITE_KEEP=30
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

> ⚠ **Hacim verisi kapsam dışı.** Bu yedekler yalnızca kontrol
> düzlemi veritabanını taşıyor: uygulama tanımları, sürümler, denetim
> zinciri. Konteynerlerin kalıcı diskleri (`/var/lib/panely/volumes`)
> DAHİL DEĞİL — panelyd o dizinleri okuyamıyor (ölçüldü, K-091).

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

⚠ **Arıza şu an hiçbir yere BİLDİRİLMİYOR.** Birim `failed` kalır ama
kimse haber almaz — alarm teslimatı hâlâ açık bir karar (K-092).
Şimdilik bu komutlara elle bakmak gerekiyor.

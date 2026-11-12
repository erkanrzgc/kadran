# GitHub bağlantısı: cihaz akışıyla özel depolar

Karar kaydı: K-167. Kullanıcı soruları atladı ("direkt başla", 12 Kasım); iki karar onundu ve
soruldu. Aşağıda **(kullanıcı)** onun kararını, **(benim kararım)** bu belgede verdiğim kararı
gösterir. Önceki tasarım taslakları: K-124 (özel depolar), K-146 (bugünkü uygulama).

## Neden

Özel depodan derleme bugün var (K-146, `docs/guides/private-git.md`), ama kurulumu üç elle adım:

1. GitHub'da depo başına ince taneli token aç.
2. Sunucuda root olarak `/etc/kadran/private-git/credentials.json` dosyasını yaz.
3. `kadran-exec.service` için ağı açan ve `--git-credentials` ile `--allow-repo` ekleyen bir
   drop-in yaz, yürütücüyü yeniden başlat.

Canlıda hiç kullanılmadı. Kurulum kolaylığı ürün önceliği (kadran-product-direction). Bu iş üç
adımı tek adıma indirir: `kadran github connect`, ekranda bir kod, github.com'da onay.

## Amaç

- `kadran github connect | status | disconnect` ve masaüstünde aynı üç iş.
- Kadran'ın GitHub App'i yalnız kullanıcının seçtiği depoları, yalnız okuyabilir.
- Jeton sunucuda doğar, sunucuda kalır. İş istasyonu yalnız kullanıcı kodunu görür; kadrand
  jetonu hiç görmez.
- Root yürütücü ağa çıkmaz; ayrıcalıklı kod küçülür.
- Özel depoda dal adıyla dağıtım çalışır (bugün `-commit` şart).

## Kapsam dışı

- Push ile otomatik dağıtım (K-125). Bu bağlantı onun ön koşulu, kendisi değil.
- Kadran'a GitHub ya da Google ile giriş. Kimlik yine SSH anahtarı ve TOTP.
- GitHub dışı hostlar (GitLab, Gitea).
- Dockerfile içinden başka özel paket indirme. Jeton derlemeye hiç girmez.
- Sunucudan jetonu GitHub'da iptal etmek (istemci sırrı ister, bkz. Bağlantıyı kesmek).

## Asla

- Jeton, yenileme jetonu ya da imzalı indirme adresi: günlükte, denetim kaydında, hata metninde,
  RPC yanıtında, iş istasyonunda.
- kadrand'nin ya da iş istasyonunun jetonu düz metin görmesi.
- Root yürütücünün internete çıkması.
- Jetonun yetkisiyle başarısız olan bir derlemenin sessizce anonim yola düşmesi (aşağıda tek
  istisna: depo bağlantının kapsamında değil).
- İstemci sırrının (client secret) Kadran'a girmesi. Cihaz akışı ve cihaz akışı jetonunun
  yenilenmesi istemci sırrı istemez (GitHub belgesi, "Refreshing user access tokens":
  `client_secret` "Required unless the user access token was generated using the device flow").

## Kararlar

### GitHub App, OAuth App değil (benim kararım)

OAuth App'in özel depo kapsamı `repo`: bütün özel depolara okuma ve yazma; salt okunur sürümü yok.
GitHub App'in kullanıcı jetonu üç kümenin kesişimine erişir: uygulamanın izinleri (yalnız
Contents: okuma; Metadata: okuma zorunlu), kullanıcının erişimi ve uygulamanın kurulduğu depolar.

### Uygulama kaydı (kullanıcı)

- Uygulamayı ben, kullanıcının oturumuyla uygulama içi tarayıcıda oluşturuyorum. "Create"
  düğmesinden önce kullanıcıdan onay alınır.
- Ayarlar:
  - Ad: "Kadran", alınmışsa yakın bir ad.
  - Ana sayfa: depo adresi.
  - Webhook kapalı.
  - Geri dönüş adresi yok.
  - "Enable Device Flow" açık.
  - "Expire user authorization tokens" açık.
  - İzin: yalnız Contents okuma (Metadata okuma zorunlu).
  - "Any account": açık kaynak ürün, başkaları da kurabilmeli.
- Özel anahtar ve istemci sırrı üretilmez.
- Client ID ve uygulamanın kısa adı (slug) gizli değildir. `kadran-git`'te sabit varsayılan
  olarak durur; kendi uygulamasını kullanmak isteyen `--client-id` ve `--app-slug` verir.

### Jetonlar 8 saatlik, sunucu yeniler (kullanıcı)

- Erişim jetonu 8 saat (`ghu_`), yenileme jetonu 6 ay (`ghr_`).
- Yenileme tek kullanımlık: kullanıldığı anda eski erişim jetonu da eski yenileme jetonu da
  ölür. Bu yüzden:
  - Yenilemeyi tek bir süreç yapar, aynı anda tek yenileme (tek uçuş).
  - Yeni çift diske atomik yazılır (geçici dosya, fsync, rename, dizin fsync), ancak ondan
    SONRA kullanılır.
  - Yazma başarısız olursa bağlantı kopmuş sayılır ve durum "yeniden bağlan" der. Sessizce
    eski jetonla devam edilmez, çünkü o ölmüştür.
- Yenileme tembel: kullanılacağı an, bitimine 5 dakikadan az kaldıysa.
- 6 ay hiç kullanılmazsa yenileme jetonu ölür; durum "yeniden bağlan" der.
- GitHub yanıtında süre alanları yoksa (uygulama sahibi süresiz jetona geçtiyse) jeton süresiz
  sayılır ve yenilenmez. İki biçim de desteklenir.

### Ayrı, yetkisiz birim: `kadran-git` (benim kararım)

Üç zorunluluk aynı yeri gösteriyor:

- kadrand `IPAddressDeny=any`: GitHub'a çıkamaz; jetonu da görmemeli.
- Yürütücü root, `AF_UNIX`, ayrıcalıklı kod 2982 / 2990 satır. Yenileme ve yazılabilir jeton
  deposu sığmaz, ağ açmak root'u internete çıkarır.
- Yenileme 8 saatte bir ve CI dağıtımında iş istasyonu yok: akış ve yenileme sunucuda olmalı.

K-124'ün baştaki önerisi (C, "ayrı çekici birim") bu. K-146 çekmeyi yürütücüye koymuştu; bu iş
onu oradan alır.

**`kadran-git`:**

- Yeni ikili `/usr/local/lib/kadran/kadran-git`.
- Kendi sistem kullanıcısı `kadran-git` (`kadran-proxy` deseni, giriş kabuğu yok). Docker
  grubunda ve `kadran` grubunda DEĞİL.
- Durum `/var/lib/kadran-git` (0700), jeton dosyası 0600.
- Ağ yalnız dışarı: `RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6`,
  `IPAddressDeny=localhost link-local multicast`, `IPAddressAllow=127.0.0.53`. Bu desen
  `kadran-notify` ve `kadran-offsite`'ta iki sunucuda da ölçüldü (K-107: DNS istisnası).
- Bağlandığı adresler koddaki sabitler: `github.com`, `api.github.com`, `codeload.github.com`.
  İstekten adres alınmaz. Tek yönlendirme bugünkü kuralla kabul edilir: yalnız beklenen
  `codeload.github.com` depo/commit yolu, ve kalıcı jeton ikinci isteğe taşınmaz.
- Kum havuzu `kadran-notify` düzeyinde: yetenek yok, `ProtectSystem=strict`, sistem çağrısı
  süzgeci, `MemoryDenyWriteExecute`.

**İki soket, iki yetki:**

| Soket | Kim bağlanır (SO_PEERCRED) | Ne yapabilir |
|---|---|---|
| `/run/kadran-git/admin.sock` | yalnız `kadran` (kadrand) | akışı başlat, durum, bağlantıyı kes, dal çöz |
| `/run/kadran-git/fetch.sock` | yalnız root (yürütücü) | commit arşivi indir |

kadrand arşiv çekemez. İçerik yolu GitHub → `kadran-git` → yürütücü (doğrular, yeniden yazar) →
Docker. kadrand arada yok.

### Yürütücü ne yapar (benim kararım)

- `PrivateGit`'in ağ ve jeton kısmı yürütücüden çıkar: HTTP istemcisi, yönlendirme denetimi,
  jeton dosyası okuma, `--git-credentials` bayrağı.
- Kalan: `fetch.sock`'a bağlanıp arşiv akışını almak ve bugünkü `rewritePrivateArchive`
  denetimleri. Bu denetimler aynen kalır:
  - sınırlar: 128 MiB sıkıştırılmış, 512 MiB açılmış, 100.000 girdi;
  - tek kök;
  - mutlak yol, `..`, symlink, hardlink, aygıt ve tekrar eden yol yok.
- `kadran-git`'in gönderdiği veri güvenilmez sayılır, denetim yürütücüde kalır. Ayrıcalıklı
  kod azalmalı; yeni sayı ölçülüp `check-exec-surface.sh`'ye yazılır, sınır aşağı çekilir.
- Karar sırası, yalnız `github.com` hostlu kaynak için:
  1. `fetch.sock` yok, ya da `kadran-git` "bağlı değil" diyor: bugünkü herkese açık yol
     (Docker uzak git adresi).
  2. "Depo kapsamda değil" (GitHub 404): herkese açık yol. Depo gerçekten özelse Docker'ın
     hatasına ipucu eklenir: "GitHub bağlantısı bu depoyu görmüyor; uygulamayı depoya kur".
  3. Başka her hata (401, 403, hız sınırı, taşıma, zaman aşımı, bozuk arşiv): derleme
     başarısız. Anonim yola düşülmez.
  4. Başarı: tar bağlamından derleme (bugünkü `ContextTar` yolu).
- Denetim kaydında kaynak yolu yazılır: `source=github-app` ya da `source=public`.

### İzin listesi (benim kararım)

- Bugün `--allow-repo` root bayrağı; kadrand onu genişletemez. Tek adımlı kurulumu kadrand
  üzerinden yapıp bu listeyi de kadrand'den doldurmak o güvenceyi siler. Yapılmıyor.
- Özel depolar için liste artık github.com'da: kullanıcının uygulamayı kurduğu depolar. GitHub
  zorluyor; ele geçirilmiş bir kadrand bu seçimi değiştiremez. K-146'nın "token'ın kapsamı
  doğrulanamaz" itirazı burada geçerli değil: kurulumun depo listesi API'den okunabiliyor.
- `--allow-repo` verilmişse bütün derlemelerde yine uygulanır (kesişim). Boşsa ek kısıt yok.
- Kurulum "All repositories" ise reddedilmez. `connect` ve `status` bunu açıkça uyarır ve
  "Only select repositories" önerir.
- `parsePrivateGit`'in "allowlist boşsa reddet" kuralı jeton dosyasıyla birlikte kalkar. Yeni
  modelde liste GitHub tarafında.

### Bağlanma akışı

1. Kullanıcı `kadran github connect` yazar (masaüstünde "GitHub'a bağlan").
2. kadrand → `admin.sock` → `kadran-git` akışı başlatır:
   - `POST https://github.com/login/device/code` (`client_id`);
   - gelen kod, adres ve bitiş zamanı kadrand üzerinden istemciye gider.
3. İstemci iki şeyi gösterir:
   - uygulamayı depolara kurma adresi: `https://github.com/apps/<slug>/installations/new`;
   - `https://github.com/login/device` adresi ve 8 haneli kod (15 dakika geçerli).
4. `kadran-git` arka planda yoklar:
   - aralık GitHub'ın verdiği; `slow_down` gelirse 5 saniye eklenir;
   - `authorization_pending` beklemeye devam;
   - `access_denied`, `expired_token`, `device_flow_disabled`, `incorrect_client_credentials`
     sonlandırır.
   - Tek akış: yeni bir başlatma eskisini iptal eder.
5. Jeton gelince:
   - `GET /user` ile hesap adı alınır;
   - `GET /user/installations` ve kurulum başına depo listesi (sayfalı, en çok 1000 depo)
     okunur;
   - durum dosyası yazılır.
6. İstemci akışı açık tutar ve sonucu gösterir: bağlanan hesap, kurulumlar, depolar, "All
   repositories" uyarısı. İstemci koparsa akış `kadran-git`'te kodun süresi bitene dek sürer.
   Kullanıcı kodu github.com'da girdiyse bağlantı kurulur, `status` gösterir.

### Bağlantıyı kesmek

- `kadran github disconnect` durum dosyasını siler ve bekleyen akışı iptal eder.
- Jetonu GitHub'da iptal etmek istemci sırrı ister (`DELETE /applications/{client_id}/token`).
  Kadran'da sır yok. Çıktı iki adresi verir:
  - yetkiyi kaldırmak için `https://github.com/settings/apps/authorizations`;
  - kurulumu kaldırmak için `https://github.com/settings/installations`.
- Bu sınır README'de ve rehberde yazılı olmalı.

### Dal çözümü

- İstemci önce bugünkü anonim çözümü dener.
- Başarısız olursa ve sunucu destekliyorsa kadrand'ye sorar: `ResolveGitHubBranch(app_id,
  branch)`. kadrand uygulamanın deposunu kendi kaydından alır; istemciden depo almaz.
- `kadran-git` sorar: `GET /repos/{owner}/{repo}/commits/{ref}`, `Accept:
  application/vnd.github.sha`.
- Dal adı sıkı doğrulanır:
  - yalnız `[A-Za-z0-9._/-]`, en çok 255 bayt;
  - `..` yok, `//` yok;
  - `/` ya da `-` ile başlamaz;
  - `/` ya da `.lock` ile bitmez.
- Yanıt tam 40 küçük onaltılık değilse reddedilir.
- Dağıtım anahtarı (CI) uygulama tanımını okuyamadığı için bugünkü gibi `-commit` verir.

### kadrand ve protokol

- Yeni çağrılar:
  - `ConnectGitHub` (sunucudan akış: önce kod, sonra sonuç);
  - `GetGitHubStatus`;
  - `DisconnectGitHub`;
  - `ResolveGitHubBranch`.
- Dördü de yalnız yöneticiye açık (K-131 izin listesi, dağıtım anahtarı reddedilir), TOTP
  oturumu ister.
- Bağlanma ve kesme kadrand denetim zincirine yazılır: hesap adı ve kurulum seçimi; jeton yok.
- Değişiklik eklemeli, protokol 3 kalır. Eski sunucuda çağrı `Unimplemented` döner; istemci
  "sunucuyu yükseltin" der.
- kadrand birimi `/run/kadran-git/admin.sock`'a ulaşabilmeli. Yürütücü birimi
  `/run/kadran-git/fetch.sock`'a ulaşabilmeli; `AF_UNIX` yeter, ağ açılmaz.

### Durum dosyası

- `/var/lib/kadran-git/github.json`, sürümlü:
  - `client_id`;
  - hesap adı ve kimliği;
  - erişim jetonu ve bitişi;
  - yenileme jetonu ve bitişi;
  - bağlanma zamanı.
- Açılışta okunur. Sahibi, kipi ve normal dosya oluşu bugünkü `readPrivateGitFile`
  sıkılığıyla denetlenir; sahibi `kadran-git` olur.
- `client_id` yapılandırmadakiyle uyuşmazsa bağlı değil sayılır.
- Bozuk dosya bağlı değil sayılır ve günlüğe yazılır. Hiçbir koşulda jeton günlüğe girmez.

### Masaüstü

- Sunucu ekranında "GitHub" bölümü:
  - bağlı değilse "Bağlan" düğmesi;
  - akış sırasında büyük kod, "Kopyala", "github.com/login/device'ı aç" ve kurulum adresi;
  - bağlıysa hesap, kurulum seçimi, depo listesi ve "Bağlantıyı kes" (onay ister).
- Yan süreçte üç yöntem.
- en/tr metinler.
- Duman testinde sahte yan süreçle: bağlı değil, bekliyor, bağlı ve eski sunucu durumları.

### Kurulum, yükseltme, sürüm

- `install.sh`:
  - `kadran-git` kullanıcısını ve grubunu oluşturur;
  - ikiliyi ve birimi kurar, birimi etkinleştirir;
  - kullanıcının `docker` ya da `kadran` grubunda olmadığını denetler (`kadran-proxy` gibi).
- `build-release.sh` sunucu ikilileri listesine `kadran-git` eklenir; yeniden üretilebilir
  derleme ve `SHA256SUMS` kuralları aynen geçerli.
- Yükseltme yürütücüyü asla başlayamaz bırakmamalı. `kadran-exec` drop-in'inde
  `--git-credentials` varsa iki yol var:
  - `install.sh` ikilileri değiştirmeden ÖNCE açık bir mesajla durur;
  - ya da yürütücü bayrağı kabul edip yok sayar ve uyarır.
  Hangisinin seçileceği planda ölçülür (ayrıcalıklı satır bütçesi). Bugünkü iki sunucuda bu
  bayrak yok (allow-repo drop-in'i var); yine de denetlenir.
- `docs/guides/private-git.md` yeni akışla yeniden yazılır. README ve CHANGELOG güncellenir.
- Bellek: boşta küçük bir Go süreci. GCP e2-micro'da (965 MiB) ölçülür.

## Test

- `kadran-git`, sahte GitHub (`httptest`) ile:
  - akış: bekleme, `slow_down` aralığı 5 sn artırıyor, ret, süre bitti, akış kapalı;
  - başarıdan sonra `/user` ve kurulum listesi;
  - yenileme:
    - dönüşüm;
    - jeton diske yazılmadan kullanılmıyor;
    - yazma hatasında bağlantı kopuyor;
    - ölü yenileme jetonunda "yeniden bağlan";
    - aynı anda iki istekte tek yenileme;
  - `client_id` uyuşmazlığı;
  - yanıt boyut sınırları;
  - yönlendirme denetimi (yürütücüden taşınan testler);
  - dal adı doğrulaması;
  - jetonun hiçbir hata metnine ve günlüğe girmediği.
- Soket yetkileri (Linux testi): `admin.sock`'a root ve başka kullanıcı giremiyor; `fetch.sock`'a
  `kadran` giremiyor.
- Yürütücü, sahte `kadran-git` ile:
  - bağlı değil ve kapsam dışı → herkese açık yol;
  - diğer hatalar → başarısız, anonim yola düşmüyor;
  - arşiv denetimleri aynen.
- kadrand:
  - dağıtım anahtarı dört çağrıda reddediliyor;
  - bağlanma ve kesme denetim zincirinde, jetonsuz;
  - `ResolveGitHubBranch` depoyu istemciden almıyor.
- Mutasyon: yeni `mutate-github.sh`, K-164 süre tablosuna eklenir.
- systemd, WSL'de gerçek birimlerle:
  - birim yeniden başlatmayı geçiyor, günlükte döngü yok;
  - `kadran-git` `api.github.com`'a ulaşıyor;
  - `169.254.169.254`'e ulaşamıyor;
  - aynı deneme birim dışında başarılı (kontrol grubu).
- `check-exec-surface.sh`: sayı düşer, yeni sınır yazılır.

## Canlı (her adım ayrı onay, öncesinde kısa durum)

1. Önce Hetzner. Sırayla:
   - yükseltme;
   - herkese açık bir depoyu tar yolundan derleme;
   - `kadran github connect`, kodu kullanıcı girer;
   - kullanıcının seçtiği özel depoyu dal adıyla dağıtma.
2. Sonra GCP. DNS çözücüsü önce ölçülür (`resolv.conf`; rehberdeki uyarı).
3. Ölçülecekler:
   - jetonun hiçbir günlükte olmadığı (`journalctl` ve iki denetim zinciri taranır);
   - yeniden başlatma sonrası bağlantının sürdüğü;
   - 8 saat sonra yenilemenin çalıştığı. Bu sonraki bir dağıtımda ya da `status`'ta
     görülür.

## Açık

- Uygulama adı GitHub'da alınmışsa yeni ad, kayıt sırasında belirlenir.
- `--git-credentials` göçünün iki yolundan hangisi: planda ölçülerek.
- Masaüstünde yerleşim: bugünkü sunucu ekranının sekmelerine uyar, ayrı tasarım turu yok.

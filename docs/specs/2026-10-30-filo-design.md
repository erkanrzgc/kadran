# Filo: birden çok sunucuyu tek yerden yönetmek ve kolay kurulum

**Tarih:** 30 Ekim 2026
**Durum:** TASARIM. Kullanıcıyla konuşularak çıkarıldı. Kullanıcının kararları **(kullanıcı)**,
benim kararlarım **(karar)** diye işaretli; itiraz edilmeyen tahminlerim de **(karar)**.
**İlgili:** K-001 (web paneli yok), K-112 (değişmeyen ikiliyi yeniden başlatmamak), K-122 (`bootstrap
-sudo`), K-131 (roller), K-155/K-156 (TOTP), K-158 (aynı ikili yeniden yazılmıyor); masaüstü
tasarımı `2026-10-07-masaustu-temel-design.md`; yol haritası faz 6 (küme).

## Neden

30 Ekim durum raporu Kadran'ı mühendislik olarak sağlam, ürün olarak dar buldu: tek sunucu, zor
kurulum. Kullanıcı yönü seçti: Kadran kişisel araç değil, açık kaynak ürün olacak; önce çok
iyi yapılacak, reklamı sonra **(kullanıcı)**.

Bugün:

- CLI her komutta hedefi ister (`kadran app list kadran-client@<adres>`); kayıtlı sunucu listesi
  yok. Yalnız TOTP oturumları sunucu başına saklanıyor.
- Masaüstünde kayıtlı sunucu listesi var (`desktop/src/renderer/lib/profile-store.ts`) ama aynı
  anda tek sunucuya bağlanıyor ve ekranlar o sunucuyu gösteriyor.
- Yeni biri sunucu kurmak için ya kaynaktan derliyor ya da yayın paketini elle açıp `bootstrap
  -repo … -binaries …` veriyor. 29/30 Ekim'deki iki yükseltme de böyle yapıldı.

## Amaç

Birden çok bağımsız Kadran sunucusunu (filo) tek pencereden yönetmek ve boş bir VPS'i terminale
girmeden Kadran sunucusuna çevirmek. Küme (tek kontrol düzlemi, işçi sunucular, uygulamaların
sunucular arasında dağıtılması) sonraki iş **(kullanıcı: "önce A, sonra B")**.

Başarı ölçütleri:

1. Yalnız root parolası verilmiş boş bir VPS (ör. Hostinger), masaüstü uygulamasının içinden,
   terminale girilmeden çalışan bir Kadran sunucusuna dönüşür ve filoda kart olarak görünür.
2. Üç sunucu tek pencereden yönetilir: kartlar, sekmeler, her sunucunun içi.
3. Üç sunucu tek düğmeyle sırayla yükseltilir; yükseltme süresince sitelere giden yoklamalar
   ölçülür ve kesinti sıfır çıkar.
4. CLI aynı listeyi kullanır: `kadran -s panely-test app list` adres yazmadan çalışır.
5. Güvenlik modeli değişmez (aşağıda "Asla").

## Kapsam dışı

- Küme ve sunucular arası uygulama taşıma (B) **(kullanıcı)**.
- Bulut sağlayıcı API'leri: Kadran Hetzner/GCP hesabına bağlanmaz, sunucu açmaz. Sağlayıcı
  adı kullanıcının elle seçtiği bir etiket **(karar)**.
- Aynı uygulamayı birden çok sunucuya birden göndermek. Filoda toplu yapılan tek iş yükseltme;
  gerisi sunucu sunucu **(kullanıcı)**.
- Web paneli (K-001).

## Asla

- Parola saklanmaz: ne diskte ne günlükte ne denetim kaydında. Yalnız kurulum süresince bellekte
  **(kullanıcı + karar)**.
- Sunucular birbirine güvenmez, birbirini bilmez. Filoyu bilen yalnız kullanıcının bilgisayarı.
- Kadran kullanıcıyı sunucusunun dışında bırakmaz: parola girişi, yeni anahtarla giriş ayrı bir
  bağlantıda doğrulanmadan kapatılmaz.
- Bugünkü sınırlar gevşemez: denetçi root, daemon root değil, istemci root değil, dışa açık port yok.

## İlkeler

**Kullanıcı dostu, ama mühendisliği görünür** **(kullanıcı: "olabildiğince user-friendly olsun,
mühendislik koksun")**. Varsayılan ekran sade dille konuşur; "Ayrıntılar" açılınca tam olarak ne
yapıldığı görünür:

- Her uzun işlem gerçek adımlarını gösterir (indirildi → SHA256 doğrulandı → kuruldu → denetimler
  ✓), tahmini çubuk değil.
- Her sonuç kanıtını taşır: "Yükseltme bitti · 0 kesinti (212/212 yoklama 200) · 3 servis yeni
  ikilide (md5 eşleşti) · ters vekil yeniden başlamadı".
- Hata saklanmaz ve sonraki adımı söyler. "Bir şeyler ters gitti" yasak.
- Sunucuyu değiştiren her işlem önce ne olacağını ve neye dokunulacağını söyler, onay ister.
- İlk bağlantıda sunucunun anahtar parmak izi gösterilir ve onaylatılır (sessiz TOFU yok).

**Hafiflik.** Kullanıcının uyarısı: yapay zekâyla yapılmış araçlar gereksiz büyük ve çok bellek
yiyor **(kullanıcı)**. Bütçeler ölçülerek konur ve CI'da sınanır:

| Ne | Bugün (ölçüldü 30 Eki) | Bütçe |
|---|---|---|
| Sunucu tarafı Kadran süreçleri (RSS toplamı, GCP e2-micro) | 88 MB (caddy 30, kadrand 28, proxy 16, exec 14) | ≤ 100 MB |
| `kadran` CLI (windows-amd64) | 13,2 MB | ≤ 20 MB |
| Masaüstü paketi (diskte) | 378 MB (Electron) | açık karar, aşağıda |
| Masaüstü boşta özel bellek | 389 MB (Electron, 6 süreç, CLI yan süreci 47 MB dahil) | açık karar, aşağıda |
| Filo yoklaması | yok | pencere görünürken sunucu başına 15 sn'de bir `Status`; gizliyken 60 sn; küçültülmüşken yok **(karar)** |

## Görsel dil (kullanıcı, 30 Ekim)

Referans taslak: [`2026-10-30-filo-taslak.html`](2026-10-30-filo-taslak.html) (tarayıcıda açılır;
pano ve sunucu sayfası). Kullanıcı dört taslak boyunca yönlendirdi; son hâli onayladı.

- **İskelet Coolify'ın (kullanıcı):** bölüm başlıklı sıkı kenar çubuğu (Filo / Güvenlik / Yönet),
  üstte seçicili yol (`Filo ⌄ / Pano ⌄`), "başlık + tek satır açıklama" bölümleri, "Son işlemler"
  tablosu (bütün sunucular), sıkı kart ızgarası, sunucu sayfasında iki katlı menü (Sunucu /
  Güvenlik / Bakım), "Sunucu özeti" ve "Bağlantı" panelleri. Coolify'ın markası, rengi ve
  metinleri alınmaz; yalnız yerleşim.
- **Malzeme sıvı cam, puslu (kullanıcı: "liquid glass", "bir tık puslu"):** pencere saydam;
  arkadaki masaüstü yalnız renk lekesi olarak seçilir. Büyük yüzeyler (pencere, kenar çubuğu,
  panel, kart) cam: güçlü bulanıklık (taslakta 26–30 px), hafif süt beyazı tül, çok ince doku
  (grain), üst kenarda ince ışık çizgisi. Küçük öğeler (menü öğesi, sekme, düğme, giriş kutusu)
  cam DEĞİL: düz dolgu ve ince kenar. Her şey kart değil (kullanıcı).
- **Köşeler köşeli (kullanıcı: "bir tık daha köşeli"):** pencere 8 px, panel ve kart 6 px, küçük
  öğeler 4 px, sayaç ve rozet 3 px. Yalnız durum hapları yuvarlak.
- **Premium (kullanıcı):** metin karakteri simge yok, 16 px ızgarada çizgi simgeler; Inter (rakamlar
  eşit genişlikte) ve teknik değerler için JetBrains Mono; tablo başlıkları ve bölüm etiketleri
  küçük, aralıklı büyük harf; ince çizgiler; ana düğmede üstten ışık alan buz mavisi dolgu.
- **Renk:** Kadran'ın kendi vurgusu buz mavisi–turkuaz (`#7dd3fc` çevresi) **(karar)**. Durum:
  yeşil hazır, kehribar uyarı, gri giriş gerekli.
- **Yerel saydamlık (karar):** Windows 11'de Mica/Acrylic, macOS'ta vibrancy (Wails'in pencere
  seçenekleri). Desteklemeyen sistemlerde (Windows 10, Linux) aynı renklerle opak koyu yüzeye
  düşer. Ayarlarda "Saydamlığı azalt" olur ve işletim sisteminin aynı ayarına uyulur.
- **Okunabilirlik (karar):** yazı, en zor arka planda (taslaktaki siyah-beyaz şerit) da okunur
  kalmalı; metne ince gölge, camın altında yeterli koyuluk. Yazı tipleri pakete gömülür (lisans
  OFL); boyutu masaüstü bütçesine sayılır.

## Parçalar ve sıra

Her parça kendi planıyla, kendi PR'ıyla ve tek başına kullanılabilir hâlde biter **(karar)**.

### 1. Sürümden kurulum (kurulum kolaylığının temeli)

- `kadran bootstrap <hedef>` artık `-repo`/`-binaries` istemez. İstemcinin kendi sürümünün sunucu
  paketini GitHub yayınından indirir, doğrular ve kurar. `-binaries` çevrimdışı kurulum için kalır.
- **Doğrulama (karar):** Aynı yerden indirilen `SHA256SUMS` yalnız bozulmayı yakalar, sahteciliği
  değil. Bu yüzden istemci, yayın derlemesinde kendi sürümünün sunucu paketlerinin SHA256'sını
  ikiliye gömülü taşır (`-X`). Sunucu paketleri istemciden önce derlenir; paket deterministik
  olduğu için (K-118) bu döngüsüz. İndirilen paket gömülü özetle eşleşmezse kurulum durur.
  İstemciye güvenen, kuracağı sunucuya da güvenmiş olur; ek anahtar yönetimi yok.
- Tek komutla istemci kurulumu: `install.sh` (Linux/macOS) ve `install.ps1` (Windows). İstemciyi
  indirir, `SHA256SUMS`'la doğrular, PATH'e koyar. Paket yöneticileri (Homebrew, Scoop) sonra.
- Kabul: taze bir VPS'e, depoyu klonlamadan, tek `bootstrap` komutuyla kurulum; kurcalanmış
  paketle kurulum duruyor (mutasyon/negatif test).

### 2. Ortak sunucu listesi

- Dosya: istemcinin yapılandırma dizininde `servers.json`; TOTP oturumlarının yanı
  (`%AppData%\kadran`, `~/.config/kadran`) **(karar)**. Yalnız kullanıcı okur (0600).
- Kayıt alanları: ad, sağlayıcı etiketi, istemci hedefi (`kadran-client@adres:port`), yönetici
  hedefi (root ya da sudo'lu kullanıcı, `-sudo` bayrağı), kullanılacak anahtar dosyasının YOLU.
  Anahtarın kendisi ve hiçbir parola dosyada yok.
- CLI: `kadran server add|list|rm|rename`; her komutta `-s <ad>`; ad yerine eski biçim hedef de
  çalışmaya devam eder.
- Masaüstü kendi profil listesini bir kez bu dosyaya taşır ve sonra yalnız onu kullanır.
- Yazma atomik (geçici dosya + rename); iki süreç aynı anda yazarsa son yazan kazanır ama dosya
  bozulmaz.

### 3. Masaüstünde filo ekranı

Görünüş "Görsel dil" bölümünde ve referans taslakta.

- Solda kenar çubuğu: Pano, Sunucular, Uygulamalar, Alarmlar; Güvenlik (Anahtarlar, Denetim);
  Yönet (Bildirimler, Ayarlar).
- Pano: "Son işlemler" tablosu ve sunucu kartları. Dağıtım ve geri almalar sunucuların kendi
  geçmişinden; yükseltmeler sunucuda kayıt bırakmadığı için istemcinin yerel kaydından
  **(karar)**.
- Sağda sunucu kartları, **sağlayıcıya göre gruplu** (Hetzner, Google Cloud, Hostinger başlıkları)
  **(kullanıcı)**. Kartta: sağlık noktası, ad, Kadran sürümü, uygulama sayısı, disk, alarm,
  oturum durumu ("Oturum açık · 7 sa" / "TOTP girişi gerekli" / "Erişilemiyor").
- Karta tıklayınca o sunucunun içi açılır: bugünkü ekranlar (Durum, Uygulamalar, Alarmlar,
  Yedekler, Denetim). "← Sunucular" ile geri dönülür **(kullanıcı)**.
- Üstte tarayıcı gibi sekmeler: "Tüm sunucular" ve açılmış sunucular **(kullanıcı)**.
- "Tüm uygulamalar" ve "Tüm alarmlar" salt okunur birleşik listeler; satıra tıklayınca ilgili
  sunucunun sekmesi açılır **(karar)**.
- Masaüstünde TOTP kodu penceresi (bugün yalnız CLI'da var); jeton CLI ile aynı dosyada.
- Kart verisi sunucunun bugünkü çağrılarından gelir (`Status`, `ListAlarms`, `ListApps`); yeni
  sunucu RPC'si gerekmez **(karar)**.

### 4. "+ Sunucu ekle"

İki yol **(kullanıcı)**:

1. **Kurulu sunucuya bağlan:** adres, `kadran-client`, anahtar, sağlayıcı etiketi. Parmak izi
   onayı, bağlantı denemesi, sürüm okuma, TOTP açıksa kod. Kart görünür.

**Sunucu anahtarları (karar, iki yol için de):** onaylanan parmak izi Kadran'ın yapılandırma
dizinindeki kendi `known_hosts` dosyasına yazılır. Her SSH çağrısı `UserKnownHostsFile` ile hem
kullanıcının `~/.ssh/known_hosts`'unu hem bu dosyayı okur; bugün güvenilen sunucular güvenilir
kalır. Kadran `~/.ssh`'a yazmaz.
2. **Boş VPS'e kur:** adres, root (ya da parolasız sudo) erişimi, sağlayıcı etiketi.
   - Erişim SSH anahtarıyla ya da **root parolasıyla** **(kullanıcı)**.
   - Parola yolu: parola bellekte tutulur → parmak izi onayı → kullanıcının açık anahtarı root'a
     yerleştirilir → **yeni bir bağlantıda anahtarla giriş doğrulanır** → ancak sonra parola
     girişi kapatılır (`sshd_config.d` dosyası) → `sshd -t` ve `sshd -T` ile etkin değer okunur
     → anahtarla bir kez daha giriş doğrulanır → parola bağlantısı kapanır. Bir adım düşerse
     parola girişi açık kalır ve kullanıcıya ne olduğu söylenir.
   - Bilinen tuzak: bulut imajlarında `50-cloud-init.conf` `PasswordAuthentication yes` taşıyor
     ve sshd ilk bulduğu değeri kullanıyor. Kadran'ın dosyası ondan önce sıralanmalı ve sonuç
     `sshd -T` ile doğrulanmalı.
   - **Erişim modeli (kullanıcı: seçenek c):** kurulum ayrı bir yönetici kullanıcısı açar
     (parolasız sudo, yalnız yönetici anahtarıyla), root'un SSH girişini ve herkes için parola
     girişini kapatır; GCP'deki düzen (K-122). İki anahtar: günlük işler `kadran-client`
     anahtarıyla (TOTP koruyor), kurulum ve yükseltme yalnız yönetici anahtarıyla. Biri
     çalınırsa öbürünün kapısı açılmaz. Kapatma sırası kilitlenme korumasına uyar: yönetici
     kullanıcısıyla sudo, yeni bir bağlantıda doğrulanmadan root girişi ve parola kapanmaz.
   - Kullanıcının anahtarı yoksa Kadran iki ed25519 anahtarı üretir (sistemin `ssh-keygen`'iyle):
     yönetici anahtarı parolalı, yalnız kurulum ve yükseltmede sorulur; günlük anahtar
     parolasız (TOTP koruyor) **(karar, kullanıcı onayladı)**.
   - **Docker (karar):** Yoksa dağıtımın kendi paketinden (`docker.io`) kurulur. Docker'ın
     deposu ve anahtarı eklenmez: root'a üçüncü taraf güven yok. GCP'deki Debian 13 bugün böyle
     çalışıyor (Docker 26.1.5+dfsg1).
   - **Dağıtımlar (karar):** yalnız ölçülmüş olanlar: Ubuntu 24.04 ve Debian 13. Diğerleri
     kurulum öncesi denetimde açık bir mesajla reddedilir; ölçüldükçe listeye eklenir.
   - Sonra 1. parçanın kurulumu koşar; adımlar ve kurulum sonrası denetimler ✓/✗ listesi olarak
     görünür. Sonunda kart eklenir ve "TOTP'yi şimdi aç" önerilir.
   - Kurulumdan sonra root erişimi saklanmaz; yalnız "root'a hangi kullanıcı ve hangi anahtar
     yoluyla gidilir" bilgisi listede kalır (yükseltme için).

### 5. Yükseltme ve toplu yükseltme

- Kartta "v0.9.1 → v0.9.2 yükselt"; ana sayfada "Hepsini yükselt" **(kullanıcı: toplu yükseltme)**.
- **Sırayla (karar):** her sunucu için önce durum ölçülür (servis md5'leri, alarmlar, denetim
  zinciri), sonra yükseltme, sonra aynı ölçüm. Yükseltme süresince istemci o sunucunun
  uygulama alan adlarını 0,5 sn arayla yoklar. Bir sunucu başarısızsa durulur, kalanlara
  dokunulmaz. Bu, 29/30 Ekim'de elle yapılan yükseltmenin düğmeye bağlanmış hâli.
- **Hedef sürüm (karar):** her zaman istemcinin kendi sürümü. Güven modeli (1. parça) yalnız onun
  özetini taşıyor. Sunucuyu daha yeni bir sürüme çıkarmak için önce istemci güncellenir.
- **Neyin durdurduğu (karar):** kurulum hatası; kurulum sonrası denetimde ✗; yükseltme sonrası
  ölçümde fark (servis yeni ikilide değil, yeni alarm, bozuk zincir); ya da yükseltmeden sonra
  30 sn içinde sitenin cevap vermemesi. Tek tük yoklama hatası durdurmaz: istemcinin kendi ağı
  da sekebilir. Ama sonuçta sayısıyla raporlanır.
- **Ters vekil ikilisi değişiyorsa (karar):** `kadran-caddy` sürüm damgası taşımıyor, normalde
  sürümler arasında aynı kalıyor ve yeniden başlamıyor. Go ya da Caddy güncellemesi onu
  değiştirirse 80/443'ü kendisi dinlediği için yeniden başlaması trafiği keser. Kadran bunu
  yükseltmeden önce söyler ve ayrıca onay ister; kesinti süresi ölçülüp raporlanır. Kesintisiz
  devir kapsam dışı.
- Sonuç kanıtıyla gösterilir (ilkelerdeki örnek).

## Masaüstü kabuğu: Wails (kullanıcı, 30 Ekim)

Karar: **Wails**. 3–5. parçalar Go ile yazılır; React arayüzü taşınır, Electron ana süreci Go'ya
geçer ve masaüstü `internal/client`'ı doğrudan çağırır.

Dağıtım **(kullanıcı: seçenek a)**: masaüstü uygulaması GitHub yayınına eklenir ve
`package-release.sh` tarafından, sunucu paketlerinden sonra, aynı deterministik düzende derlenir.
Yeni sürüm çıkınca "güncelleme var" der, kendini sessizce güncellemez. Kod imzası sonra.

Karar öncesi ölçüm:

Hafiflik bütçesi bugünkü kabuğa takılıyor. 30 Ekim'de ölçüldü (Windows 11; uygulama açılıp 20 sn
beklendi, kendisi ve bütün alt süreçleri `Win32_Process` ağacından toplandı):

| | Electron (bugünkü uygulama, 12 Ekim paketi) | Wails v2.17.0 denemesi |
|---|---|---|
| Diskte | 378 MB (klasör) | 10,7 MB (tek dosya; WebView2 Windows'la geliyor) |
| Süreç | 6 (CLI yan süreci dahil) | 7 (1 + 6 WebView2) |
| Özel bellek | 389 MB (yan süreç 47 MB) | 209 MB |
| Çalışma kümesi toplamı | 485 MB | 413 MB (paylaşılan sayfalar iki kez sayılıyor) |

Kıyas bire bir değil: Electron gerçek arayüzü ve bir sunucu bağlantısıyla, Wails denemesi tek
statik ekran ve Go'dan gelen veriyle ölçüldü. React arayüzü Wails'e taşınınca bellek bir miktar
artar. Sonuç: disk 35 kat küçülüyor, bellek kabaca yarıya iniyor. WebView2 de Chromium olduğu
için bellekte mucize yok. Seçenekler:

- **Electron'da kalmak:** iş en az. Disk ve RAM bütçesi tutmaz.
- **Wails'e geçmek (önerim):** Go ve işletim sisteminin kendi web görünümü (Windows'ta WebView2).
  React arayüzü (3.865 satır) büyük ölçüde kalır; Electron ana süreci (1.694 satır TypeScript)
  Go'ya yazılır. Masaüstü artık CLI'ı yan süreç olarak çalıştırmaz, `internal/client`'ı doğrudan
  çağırır. Tek dil, daha küçük paket. Bedeli: ana süreç yeniden yazılır; cam saydamlık gibi yerel
  pencere özellikleri bire bir taşınmayabilir.
- **Tauri:** Windows'ta aynı WebView2'yi kullanıyor, bellekte Wails'ten farkı olmaz; üstüne Rust,
  yani ikinci bir derleme zinciri.
- **Yerel Go arayüzü (Fyne ya da Gio):** web görünümü yok, tek süreç; bellek büyük olasılıkla
  daha da düşük (ölçülmedi). Bedeli: React arayüzü atılır, ekranlar baştan yazılır, cam görünüm
  zorlaşır.

**Bütçe (karar):** Wails seçilirse paket ≤ 20 MB, 5 sunucuyla boşta özel bellek ≤ 250 MB.
Electron'da kalınırsa ikisi de bugünkü değerin altında tutulur (≤ 380 MB, ≤ 390 MB).

Karar 3. parçadan önce gerekli.

## Riskler

- Kapsam büyük. Beş parça ayrı ayrı biter; 1. ve 2. parça masaüstü olmadan da değer üretir.
- Parola ile SSH yeni bir yol: bugünkü istemci sistemin `ssh`'ını `BatchMode=yes` ile çağırıyor.
  Parola bağlantısı için `golang.org/x/crypto/ssh` (go.mod'da zaten dolaylı bağımlılık) doğrudan
  kullanılır; yalnız bu yolda.
- GitHub'a ulaşılamazsa kurulum düşer: `-binaries` ile çevrimdışı yol kalır, hata bunu söyler.
- Farklı dağıtımlar: `sshd_config.d` olmayan eski sistemler. Kurulum öncesi denetim bunu bulur ve
  durur.

## Ölçüm

- Güvenlik sınırına dokunan iki parça mutasyonla sınanır: 1. parça (gömülü özet doğrulaması) ve
  4. parça (parola kapatma sırası, kilitlenme koruması, parolanın günlüğe düşmemesi).
- Parola yolu gerçek bir taze VPS'te uçtan uca denenir (satın alma kullanıcı onayıyla).
- Hafiflik bütçeleri CI'da: CLI ikilisinin boyutu ve masaüstü paketinin boyutu sınırı aşarsa kırmızı.
- Toplu yükseltme iki gerçek sunucuda, iki noktadan yoklanarak ölçülür.

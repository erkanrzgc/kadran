# Masaüstü — 1. parça: temel ve cam tasarım

**Tarih:** 7 Ekim 2026
**Durum:** TASARIM. Kullanıcı bölümleri tek tek onayladı; bu belgenin onayı bekleniyor.
**İlgili:** K-001 (masaüstü + CLI, web paneli yok), K-141 (İngilizce; bu parça 4. aşamayı
kapatır), yeni karar kaydı K-142 (uygulamayla birlikte yazılacak).

## 1. Amaç

Kullanıcı masaüstü uygulamasını baştan istiyor: pencere cam gibi olacak, arkadaki masaüstü
görünecek. Uygulama zamanla CLI'ın yaptığı her şeyi yapacak. Bu iş dört parçaya bölündü;
her parçanın kendi tasarımı, planı ve PR'ı var:

1. **Temel ve cam tasarım (bu belge):** çatı, pencere modları, Görünüm ayarları, yazı
   tipleri, dil, kabuk ve bugünkü iki ekran (Status, Audit) ile Settings.
2. **Okuma ekranları:** uygulamalar ve sürümler, alarmlar, yedekler, canlı loglar, alan
   adı kontrolü.
3. **Değişiklik yapan işlemler:** deploy, rollback, uygulama oluşturma/güncelleme/silme,
   budama, yedek alma. Masaüstü ilk kez sunucuyu değiştirebilecek. Bu yüzden ayrı bir
   güvenlik kararı ve onay pencereleri gerekecek.
4. **Sunucu kurulumu:** bootstrap ve anahtar yönetimi. En sona kalıyor; root ve sudo
   gerektirdiği için CLI'da da kalabilir.

### Kullanıcının söyledikleri

- Liquid glass: her şey cam gibi; pencere cmd'deki gibi saydam olacak.
- Denemedeki ayar kaydırıcıları kalıcı olarak kullanıcıya açık kalacak.
- Bütün sistem yazı tipleri seçilebilecek. JetBrains Mono olacak; kullanıcı JetBrainsMono
  Nerd Font Medium kullanıyor.
- Odak ve hover efekti belirgin olacak.
- Ad **KΛDRΛN**. Logo sonra, bir grafik tasarımcıya yaptırılacak.
- Görsel taban, denemenin ilk cam kart görünümü. "Gösterge paneli" yönü reddedildi.
- Electron'da kalınıyor. Arayüz React + Motion ile yazılıyor; mevcut kabuğun içinde
  (yaklaşım A).
- İkon ana seti lucide-animated.
- Uygulama Türkçe ve İngilizce; varsayılan İngilizce, tek sürüm.
- Clear modda pencere kenardan boyutlandırılabiliyor; kullanıcı denedi.
- Settings sol menüde; ayarlar o sayfada. Sol panelin altında `KΛDRΛN v0.5.1` yazar.
- Coolify'ın düzeninden alınacak. Kullanıcı izin aldığını söyledi; Coolify Apache-2.0.
  Alınan şey düzen; kod, logo ve marka rengi alınmaz. Ayrıntısı §5'te.

### Varsayımlar (kullanıcı düzeltebilir)

- Birincil platform Windows 11. macOS ve Linux derlenir ve testten geçer, görsel kontrolü
  sonra yapılır.
- 1. parçada masaüstü hâlâ salt okunur. README'deki "the desktop app is read-only" cümlesi
  doğru kalır.
- "AI gibi durmasın" isteği arayüz metnine uygulanır: kısa, doğal, pazarlamasız.

## 2. Kapsam

**İçinde:**

- React + Vite + Motion'a geçiş.
- Pencere modları ve Görünüm ayarları; yazı tipi seçimi ve gömülü JetBrains Mono.
- Dil katmanı (en/tr).
- Kabuk, Status, Audit ve Settings ekranları.
- İzinlerin sıkılaştırılması.
- Windows paketleme.
- Testler, mutasyon betiği, CI işi ve belgeler.

**Dışında:**

- 2–4. parçalar.
- Logo.
- Kurulum sihirbazı, imzalama, otomatik güncelleme.
- macOS ve Linux'un görsel olarak doğrulanması.
- Görsel ince ayar turu (kullanıcı "sonra bakarız" dedi).

## 3. Yapı

Korunanlar:

- Ana süreç (`src/main/index.ts`).
- Sidecar istemcisi (`src/shared/sidecar-client.ts`) ve profiller (`src/main/profiles.ts`).
- Mevcut 26 test ve duman testi.
- Electron paket adı `kadran-desktop`. Değişmez: kullanıcı verisi dizini bu addan türüyor,
  değişirse profiller kaybolur (K-136).

| Birim | Görev |
|---|---|
| `src/main/window.ts` (yeni) | Pencereyi kurar. İşletim sistemine göre hangi modların sunulacağını belirler. Mod değişimini uygular: Clear ile diğerleri arasında pencere yeniden kurulur, çünkü saydamlık sonradan açılıp kapanamıyor. Acrylic ↔ Mica `setBackgroundMaterial` ile anında değişir. Mod → pencere seçenekleri eşlemesi saf bir fonksiyondur ve test edilir. |
| `src/main/appearance.ts` (yeni) | `appearance.json`'ı `profiles.json`'ın yanında okur ve yazar. Her alanı doğrular; bozuk ya da eksik alan varsayılana döner. Atomik yazar (geçici dosya + yeniden adlandırma), profillerle aynı desen. |
| `src/main/permissions.ts` (yeni) | Yalnız `local-fonts` iznini verir, gerisini reddeder. İstek ve kontrol işleyicilerinin ikisi de buradan kurulur. |
| `src/preload/index.cjs` | Beyaz liste büyür. Eklenenler: Görünüm okuma/yazma, pencere düğmeleri (küçült/büyüt/kapat) ve mod değiştirme. Her girdi ana süreçte doğrulanır. |
| `src/renderer/` | React ile yazılır, Vite ile `out/renderer`'a derlenir. Klasörler: `screens/` (Status, Audit, Settings), `components/` (Titlebar, Rail, ServerList, Nav, Card, ChainCard, AuditTable, Toast, Appearance), `lib/` (format, chain, fonts, i18n), `icons/` (kopyalanan lucide-animated), `i18n/en.json` + `tr.json`, `styles/`. Dosyalar 400 satırın altında tutulur. |

Derleme: `tsc` (ana süreç ve paylaşılan kod) + `vite build` (arayüz) + preload kopyası.
Geliştirmede de canlı sunucu yok, `vite build --watch` kullanılır. Böylece CSP'ye geliştirme
istisnası açılmaz.

Sidecar ve veri akışı değişmez: `version`, `status`, `audit.list`, `audit.verify`.

## 4. Pencere, Görünüm, yazı tipleri

### Modlar

| Sistem | Sunulan modlar | Varsayılan |
|---|---|---|
| Windows 11 22H2+ | Clear, Acrylic, Mica, Solid | Acrylic |
| Windows 10 | Clear, Solid | Clear |
| macOS | Clear, Vibrancy, Solid | Vibrancy |
| Linux | Clear, Solid | Solid |

- Pencere çerçevesiz; başlık çubuğunu biz çiziyoruz. macOS'ta trafik ışıkları sistemin.
- Clear modu `transparent: true` ile açılır.
- Acrylic ve Mica `backgroundMaterial` ile açılır.
- Solid opak bir yedektir; düşük donanım ve okunurluk için.
- Desteklenmeyen bir mod kayıtlıysa sistemin varsayılanına dönülür.

### Görünüm ayarları (`appearance.json`, sürüm 1)

| Alan | Değer | Varsayılan |
|---|---|---|
| `mode` | clear · acrylic · mica · vibrancy · solid | yukarıdaki tablo |
| `tint` | 0–100 | 60 |
| `panel` | 0–30 | 7 |
| `edge` | 0–60 | 22 |
| `glow` | 0–100 | 60 |
| `accent` | ice · amber · phosphor · paper | ice |
| `tone` | dark · light | dark |
| `language` | en · tr | en |
| `uiFont` | `{ family: string ≤ 200, weight: 100–900 }`; boş aile = sistem yazı tipi | sistem, 400 |
| `codeFont` | aynı biçim; boş aile = gömülü JetBrains Mono | JetBrains Mono, 400 |

- Değişiklikler anında görünür.
- **Reset to defaults** düğmesi var.
- Klavye odak halkası `glow` 0'da bile görünür kalır.
- Sistemde "hareketi azalt" açıksa geçişler kapanır.

### Yazı tipleri

- Kurulu yazı tipleri `queryLocalFonts()` ile listelenir; izni `permissions.ts` verir.
- Seçilen ailenin her yüzü `FontFace` ile `local("<postscriptName>")` kaynağından, kendi
  kalınlığıyla kaydedilir. Sahte kalınlaştırma kapalıdır (`font-synthesis: none`).
- Başlıklar, ailedeki bir üst kalınlığı kullanır.
- JetBrains Mono woff2 (400, 500, 700) uygulamaya gömülüdür. `font-src 'self'` yeterli.
- Liste alınamazsa sistem yazı tipi ve gömülü JetBrains Mono kullanılır.

### Görsel taban

Denemedeki ilk cam görünüm:

- Yuvarlak cam kartlar: gradyanlı panel dolgusu, ince parlak kenar, üst kenar ışığı.
- Mavi (ice) vurgu; durum renkleri yeşil, sarı, kırmızı.
- Hover'da hafif kalkma ve parlama. Klavye odağında iki katlı halka.

İnce ayar sonraki bir turda yapılacak.

## 5. Ekranlar

**Kabuk.**

- Başlık çubuğu: `KΛDRΛN` yazısı (ekran okuyucu için `aria-label="Kadran"`), logonun boş
  yeri ve pencere düğmeleri.
- Sol cam panel, yukarıdan aşağı:
  - Hedef kutusu ve Connect.
  - Kayıtlı sunucular. Durum noktası: yeşil = bağlı ve sorunsuz, sarı = uyarı, gri =
    bakılmadı.
  - Menü: Status, Audit, Settings.
  - En altta `KΛDRΛN v0.5.1`: sidecar'ın bildirdiği Kadran sürümü. Sidecar ve protokol
    ayrıntısı Settings → About'ta durur (kullanıcı isteği).

**Status.**

- Daemon, Executor ve Host kartları.
- Daemon root çalışıyorsa kırmızı `BROKEN INSTALL`.
- İki zincir kartı. Rozetler CLI ile aynı: `VALID` yeşil, `BROKEN` kırmızı, `UNVERIFIABLE`
  sarı, `UNKNOWN` gri. Verify düğmesi ve zincirlerin neden ayrı olduğunu anlatan not.
- Son 10 denetim kaydı ve "tümünü gör" bağlantısı.

**Audit.**

- Tam tablo; Load older ile 50'şer kayıt geriye gider (`after_seq`).
- Eylem ve sonuca göre süzme; bu arayüz tarafında yapılır.
- Parmak izine tıklayınca kopyalanır.

**Settings** (sol menüde; yüzen ayar paneli yok, kullanıcı isteği). Tek sayfa, satır
düzeni: solda ad ve tek satırlık açıklama, sağda denetim.

- **Appearance:** bütün kaydırıcılar, seçimler ve Reset.
- **Language:** English / Türkçe.
- **Servers:** kayıtlı sunucuyu silme.
- **About:** uygulama, sidecar ve protokol sürümleri.

### Coolify'dan alınanlar

Kullanıcının gösterdiği Coolify sunucu ve analitik ekranından 1. parçaya uyanlar:

- **Üst çubuk.**
  - Solda kırıntı yolu: `Servers / hetzner ▾`. Ok bir sunucu seçici açar.
  - Yanında sağlık hapı:
    - yeşil **Healthy**;
    - sarı **Attention required**: executor erişilemez ya da bir zincir UNVERIFIABLE;
    - kırmızı **Tampering suspected**: bir zincir BROKEN;
    - kırmızı **Broken install**: daemon root çalışıyor.
  - Sağda gecikme hapı (`41 ms`): `status` çağrısının gidiş-dönüş süresi. Ağdaki ssh
    süresi değil, etikette de öyle yazar.
  - En sağda birincil eylem **Verify chains**.
- **Overview şeridi.** Status'un tepesinde tek cam panelde yan yana karolar: küçük büyük
  harfli etiket, büyük değer ve altında küçük bir çizgi grafik. Karolar Uptime, Memory,
  Round-trip, Executor, Chains. Grafikler yalnız **Memory** ve **Round-trip** için ve
  yalnız ekran açıkken toplanan son 60 örnekten çizilir. Örnekler bellekte durur, diske
  ya da sunucuya yazılmaz. Daemon, Executor ve Host ayrıntı kartları şeridin altında kalır.
- **Live refresh.** Yeşil noktalı anahtar. Açıkken Status her 10 saniyede yenilenir;
  varsayılan kapalı.
- **Bölüm başlıkları.** Panellerin başlığı altı çizili küçük yazı (Coolify'daki
  "Overview", "Requests" gibi).
- **Audit'te sonuç çubuğu.** Yüklü kayıtların sonuç dağılımı tek yatay çubukta: ok, denied,
  failed; renkli ve sayılı. Yanında aralık seçici: `last 50 / 200 / 1000`.
- **Settings'te gruplu bölümler.** Appearance, Language, Servers, About; Coolify'ın gruplu
  ayar listesi gibi başlıklarla.

Sonraki parçalara kalanlar:

- **Trafik analitiği** (istek, ziyaretçi, hata oranı). Kadran'da bu veri yok; yeni bir
  sunucu özelliği gerekir.
- **"Top" listeleri** (2. parça, uygulamalar).
- **İki sütunlu, zorunlu alan işaretli formlar** (3. parça).

**Durumlar.** Yükleniyor, boş, hata ve bağlantı koptu durumlarının her biri görünür.
Sunucudan gelen metin her zaman düz metindir; React metni kaçırarak yazar.

**Metin dili.** Kısa ve doğal. Pazarlama kalıbı yok. Durum etiketleri ve süre biçimi
(`2d 3h`) CLI ile aynı.

## 6. Dil

- `i18n/en.json` (varsayılan) ve `i18n/tr.json`.
- Kendi `t(key, params)` fonksiyonumuz: `{ad}` yer tutucuları ve bir/çok biçimi. Kütüphane
  yok.
- Sunucudan gelen hata metinleri İngilizce kalır (K-141 3. aşama); Türkçe arayüzde de
  İngilizce görünür.
- CLI yalnız İngilizce kalır.
- K-141 "dil seçimi yok" diyordu; masaüstü için bu karar değişiyor ve K-142'de
  gerekçelendirilecek. Gerekçe: kullanıcı istedi; CI iki dosyanın anahtar eşitliğini
  sınadığı için test matrisi ikiye katlanmıyor.

## 7. Güvenlik ve bağımlılıklar

**Yeni paketler** (sürümler kilitli, `npm ci`):

- Çalışma zamanı: react, react-dom, motion (MIT).
- Yalnız derleme: vite, @vitejs/plugin-react (MIT).
- Yalnız paketleme: @electron/packager (BSD-2).
- Dil kütüphanesi ve arayüz kiti yok.

**Depoya kopyalananlar:**

- lucide-animated ikonları (MIT; çizimler Lucide, ISC). Yalnız kullanılanlar, lisans
  başlığıyla.
- JetBrains Mono (OFL-1.1) ve lisans metni.

**Lisans kontrolü.** Bir test, `package-lock.json`'daki çalışma zamanı paketlerinin ve
kopyalanan dosyaların lisansını izinli listeyle karşılaştırır: MIT, ISC, BSD-2/3,
Apache-2.0, OFL-1.1. CI `npm audit --omit=dev` koşar.

**Korunanlar:**

- `sandbox`, `contextIsolation`, `nodeIntegration: false`, `webviewTag: false`.
- Gezinme kilidi ve harici bağlantıların tarayıcıda açılması.
- CSP aynı: `script-src 'self'`, `style-src 'self'`, satır içi kod yok.
- Derlenmiş `out/renderer/index.html`'de satır içi betik ve `style=` niteliği olmadığını
  bir test ölçer. Vite'ın varsayılan çıktısına güvenilmez.

**Sıkılaşanlar:**

- İzinler: yalnız `local-fonts`. Bugün Electron her izni veriyor.
- `dangerouslySetInnerHTML` ve `innerHTML` kullanımı testle yasak.
- Köprüye eklenen her kanal ana süreçte doğrulanır: mod izinli değerlerden biri, sayılar
  sınırları içinde, yazı tipi adı 200 karakteri geçmez.

## 8. Paketleme

- `npm run dist` → `@electron/packager` ile Windows x64 klasörü. İçinde `Kadran.exe`
  (pencere başlığı KΛDRΛN) ve `resources/kadran.exe` (sidecar; `go build` ile üretilir).
- `spawn-sidecar.ts` bu yolu zaten destekliyor.
- İmzasız; ilk açılışta SmartScreen uyarısı verir.

## 9. Testler ve CI

**Birim testleri** (`node:test`, yeni bağımlılık yok):

- appearance: kırpma, bozuk dosya, atomik yazma.
- Pencere modları: işletim sistemi → sunulan modlar → pencere seçenekleri.
- i18n:
  - `en` ve `tr` anahtar eşitliği.
  - `en`'de Türkçe harf yok.
  - Arayüz kodunda çeviri dışı JSX metni yok (TypeScript AST taraması; `KΛDRΛN` gibi
    istisnalar adıyla listelenir).
- format ve chain: süre, bayt, parmak izi; etiketler CLI ile aynı.
- fonts: stil adı → kalınlık.
- health: daemon kullanıcısı, executor ve zincir durumu → sağlık hapı. BROKEN her zaman
  Attention'ı ezer.
- Örnek tamponu: en fazla 60 örnek; en eski düşer.
- permissions: yalnız `local-fonts`.
- Yasak kalıp taraması (`innerHTML`, `dangerouslySetInnerHTML`).

**Uçtan uca.** Duman testi büyür: gerçek Electron açılır, sahte bir sidecar hazır cevap
verir (`KADRAN_SIDECAR_COMMAND`). Kontrol edilenler:

- Status kartları, rozetler ve denetim satırları çiziliyor.
- `BROKEN` ve `UNVERIFIABLE` farklı sınıfta.
- Dil Türkçe'ye geçince metin değişiyor.
- Konsolda CSP ihlali yok.
- Köprü yüzeyi beklenen listede; Node sızmıyor.
- `notifications` ve `media` izinleri reddediliyor.

**Mutasyon** (`scripts/mutate-desktop.sh`). Her mutant yakalanmalı:

- sandbox kapalı;
- izin verici her şeye evet diyor;
- CSP'ye `'unsafe-inline'` eklendi;
- `tr.json`'dan bir anahtar silindi;
- `en.json`'a Türkçe kelime girdi;
- arayüzde `innerHTML` kullanıldı;
- mod doğrulaması gevşedi;
- tint sınırı kaldırıldı.

**CI, yeni "Masaüstü" işi:**

- ubuntu-latest: `npm ci`, `tsc --noEmit`, birim testleri, `vite build`, lisans kontrolü,
  `npm audit`, xvfb altında uçtan uca (Solid mod).
- windows-latest: uçtan uca ve paketleme.

**Elle kontrol listesi** (PR'da işaretlenir):

- Windows 11'de Acrylic ve Mica cam görünüyor.
- Clear modda kenardan boyutlandırma çalışıyor.
- JetBrainsMono Nerd Font Medium seçilebiliyor.
- Klavyeyle gezinmede odak halkası görünüyor.
- Paketlenmiş `Kadran.exe` sidecar'ı bulup bir sunucuya bağlanıyor.

## 10. Belgeler

- **K-142:** Electron'da kalma gerekçesi; React + Motion; pencere modları; izin
  sıkılaştırması; masaüstünde dil seçimi.
- **K-141:** 4. aşama bölümü ve durum satırı.
- **README:** masaüstü tanımı. "Electron + React" artık doğru oluyor; bilinen eksikler
  listesi de güncellenir.
- **CHANGELOG:** yeni masaüstü.

## 11. Denemenin ölçtükleri

Atılacak deneme `scratchpad/cam-deneme` içindeydi; Kadran koduna girmedi.

- Clear modu, Electron 43 / Windows 11'de çerçevesiz ve saydam pencereyle çalıştı:
  masaüstü görünüyor, kenardan boyutlandırılabiliyor (kullanıcı denedi).
- Acrylic çerçevesiz pencerede de bulanık cam olarak göründü (ekran görüntüsü). Mica
  neredeyse opak koyu göründü.
- `queryLocalFonts()` `file://` sayfasında 454 yüz / 190 aile döndü. `data:` sayfasında
  çalışmadı, sayfa yerel dosyadan yüklenmeli.
- "JetBrainsMono Nerd Font · Medium" (`JetBrainsMonoNF-Medium`) `FontFace` + `local()` ile
  yüklendi.

## 12. Açık konular

- Logo: tasarımcıdan gelecek. Gelene kadar başlıkta boş bir yer durur. iStock gibi stok
  görseller lisansları gereği logo olarak kullanılamaz.
- Görsel ince ayar turu.
- macOS ve Linux'ta görsel doğrulama.
- Kurulum sihirbazı, imzalama, otomatik güncelleme.

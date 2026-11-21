# Kadran Konsolu: masaüstünde Kadran'ın komut diliyle çalışan alt panel

Karar kaydı: K-169. Beyin fırtınası 18 Kasım 2026; bağımsız okuyucu denetimi ve son kararlar
19 Kasım 2026. Aşağıda **(kullanıcı)** onun kararını, **(benim kararım)** bu belgede verdiğim
kararı gösterir. İlgili kayıtlar: K-165 (Wails, köprü beyaz listesi), K-162 (kayıtlı sunucular,
`-s`), K-156 (TOTP), K-167 (GitHub bağlantısı).

## Neden

Kullanıcı masaüstü uygulamasında bir terminal istedi. Sunucuya ya da konteynere gerçek kabuk
açmak reddedildi: `kadran-client` anahtarı `command="kadran-connect",restrict` ile kabuk
açamaz, bu Kadran'ın asıl farkıdır (container-is-not-a-vm). Kullanıcının itirazı: "Kadran
açmazsa kabuğu havalı olmuyor." Uzlaşma: terminal gibi görünen ve hissettiren, ama içinde
Kadran'ın kendi komut dilinin çalıştığı bir konsol **(kullanıcı)**. Komut dili CLI'ınkinin
aynısı, `kadran` öneki olmadan **(kullanıcı)**.

Başarı: masaüstünden ayrılmadan `deploy`, `logs -f`, `alarms`, `login` yazılabilmesi; konsolun
izin verdiği her komutun CLI'daki çıktıyla çalışması (iki istisna aşağıda, Amaç'ta); GUI
düğmelerinin güvenlik kapısının konsolda delinmemesi.

## Amaç

- Masaüstünde sunucu ekranının altında açılıp kapanan **alt panel** **(kullanıcı)**.
- Konsolda iki komut grubu **(kullanıcı: "a ve b olsun")**:
  - **(a)** `kadran-client` yolundan gidenler: `status`, `app …`, `deploy`, `rollback`, `logs`,
    `prune`, `alarms`, `backup …`, `domain check`, `audit …`, `github …`, `login`, `logout`,
    `totp status|enable|disable`, `server list`.
  - **(b)** root ya da sudo yolundan gidenler: `key …`, `totp reset`. Ek güvenlik katmanlarının
    arkasında (aşağıda). `bootstrap` bu gruptaydı, konsoldan çıkarıldı **(kullanıcı, 19 Kasım)**:
    kapsam dışına bakın.
- Dil, bayraklar, yardım metni ve çıktı CLI ile **birebir aynı**: aynı kod çalışır **(kullanıcı:
  yaklaşım 1)**. İki istisna (19 Kasım):
  - `version` masaüstünün sürümünü yazar, sunucu paketi satırları yoktur (Tasarım 1);
  - `totp enable`'ın sırrı, QR'ı ve kurtarma kodları konsol akışında değil, ayrı kartta görünür
    (Tasarım 6a).

## Kapsam dışı

- Gerçek kabuk: sunucuda, konteynerde ya da iş istasyonunda (`cmd`, PowerShell) komut çalıştırma.
- Kabuk özellikleri: boru (`|`), yönlendirme (`>`), `;`, `&&`, değişken, joker genişletme,
  betik, döngü. Konsol tek satır, tek komut çalıştırır.
- Birden çok konsol sekmesi ya da ayrı konsol sayfası (seçenek C, seçilmedi). Açılır perde
  (seçenek B, seçilmedi).
- Konsoldan `server add|rm|rename`: sunucu ekleme ve silme arayüzdeki akışta kalır **(benim
  kararım: liste değişikliği yerel onay akışını zaten GUI'de yaşıyor, konsolda ikinci bir yol
  açmaya değmez)**. `server list` okuma olduğu için var.
- `bootstrap` **(kullanıcı, 19 Kasım)**. CLI, yayından indirdiği sunucu paketini ikiliye
  gömülü SHA256 özetleriyle doğrular (`version.ServerSHA256*`, `cmd/kadran/main.go:416-425`,
  `internal/bootstrap/release.go:60-61`). Masaüstü derlemesi yalnız `Version` ve `Commit`
  damgalar (`desktop/scripts/dist.ts:30`), sürümle yayımlanmıyor, sürüm metni
  `git describe` çıktısı. Konsolda `bootstrap` ya hiç çalışmaz ya da masaüstünü sürüm hattına
  bağlamayı gerektirirdi. Seyrek, dakikalar süren, dosya yükleyen bir komut: CLI'da kalır,
  konsol "CLI'da çalıştır" der.
- `audit verify -anchors` **(benim kararım)**: iş istasyonundan dosya okur ve okuma sınıfında
  onay kutusu yok; bayraksız `audit verify` konsolda var.
- `sidecar` ve `version` dışındaki tanı komutları; `sidecar` konsolda yasak (stdio sunucusu).
- Root yoluna TOTP eklemek: root yolu kadrand'dan geçmez; bilgisayarı ele geçiren SSH
  anahtarını zaten doğrudan kullanır. Koruma eklemeden zahmet ekler (kullanıcı onayladı).
- Masaüstünün oturumsuz sunucuyu 30 sn'de bir yoklaması (18 Kas bulgusu) ve açılıştaki sahte
  `proxy_unreconciled` alarmı: ayrı işler.

## Asla

- Konsoldan, aynı işlem için arayüz düğmesinin istediği yerel onayı istemeden durum değiştirmek.
- Sınıflandırılmamış bir komutu çalıştırmak (yeni bir CLI komutu, sınıfı yazılmadan konsolda
  açılmaz).
- Geçmiş dosyasına gizli değer yazmak: `app create` ve `app update`'teki `-env KEY=VALUE`
  değerleri, istemlere girilen TOTP kodu ya da kurtarma kodu.
- Root SSH anahtarını, parolasını ya da TOTP sırrını masaüstünde saklamak.
- TOTP sırrını, QR kodunu ya da kurtarma kodlarını gizli çıktı kartı dışında göstermek; çıktı
  tamponuna, geçmişe ya da herhangi bir kayda yazmak (Tasarım 6a).
- Konsol çıktısını iş istasyonu dışına göndermek.

## Yaklaşımlar

1. **CLI'ın komut kodunu ortak pakete taşımak** — seçilen **(kullanıcı)**. Aynı kod, aynı dil,
   aynı çıktı; yeni komut konsola kendiliğinden gelir; konsol sunucu başına açık tuttuğu
   bağlantıyı kullanır. Bedeli: tek seferlik taşıma (test dışı 28 dosya, 5.265 satır; testlerle
   54 dosya, 10.956 satır) ve masaüstünde bir sınıflandırma ve onay katmanı.
2. Konsolda ayrı ayrıştırıcı + köprünün hazır işlemleri. Güvenlik yolu GUI ile aynı olurdu, ama
   dil ve çıktı CLI'dan kopar; eksik işlemler (prune, domain check, totp, key, bootstrap) için
   yeni köprü yöntemleri gerekirdi. Reddedildi: "aynı dil" sözü sürekli eşleme borcuna döner.
3. Kurulu `kadran.exe`'yi alt süreç olarak çalıştırmak. En hızlı; ama ayrı kurulum ve sürüm
   eşleşmesine bağımlı, her komut yeni SSH bağlantısı (~1 sn), istem ve onaylar süreçler arası
   taşınmak zorunda. Reddedildi.

## Tasarım

### 1. Ortak komut paketi

Bugün `cmd/kadran` (package main) test dışı 28 dosya, 5.265 satır. `cli` yapısı
stdin/stdout/stderr'ı ve dış bağımlılıkları (`dial`, `sessions`, `servers`, `keys`, `now`, …)
zaten alan olarak taşıyor. `os.Exit`, `os.Stdin` ve sinyallere yalnız `main.go` dokunuyor. Ama
süreç geneline dokunan başka yerler de var (19 Kasım denetimi):
- komutlar ortam değişkenlerini ve ev dizinini doğrudan okuyor (`main.go:416-417, 468, 476`);
- `internal/client` dışında `ssh`'ı konsol penceresini gizlemeden başlatan yerler var
  (`cmd/kadran/domaincheck.go:70`, `internal/bootstrap/bootstrap.go:495`, `cmd/kadran/upload.go:182`).
  `-H windowsgui` ile derlenen masaüstünde bunlar bir an konsol penceresi açar.

- Komut kodu `internal/cli` paketine taşınır **(benim kararım: ad)**. `cmd/kadran/main.go`
  incelir: argümanları, `os.Std*`'ı ve sinyal bağlamını pakete verir, dönen çıkış koduyla çıkar.
- Paket dışa şunu açar **(benim kararım: imza; ayrıntısı uygulayıcının)**:
  - `Run(ctx, args []string, env Env) int`. `Env` şunları taşır:
    - `Stdin io.Reader`, `Stdout`, `Stderr io.Writer`;
    - `Sensitive io.Writer`: TOTP sırrı ve kurtarma kodları buraya yazılır; CLI'da `Stdout`'tur
      (Tasarım 6a);
    - bugünkü bağımlılık alanları;
    - ortam değişkeni okuyucu ve yapılandırma/ev dizini (komutlar `os.Getenv`'i doğrudan
      çağırmaz);
    - harici `ssh` komutunu kuran kanca: masaüstü pencereyi gizler, CLI bugünkü gibi.
    Boş alan bugünkü davranış demektir.
  - `Commands() []Command` — ad, alt komutlar, bayraklar (ad, tür, açıklama) ve **sınıf**.
    Tamamlama, sınıflandırma ve onay bunu okur; komutu çalıştırmadan bayraklar çıkarılabilmeli
    ve satır ayrıştırılabilmeli. Bugün bayraklar `run…` içinde tanımlanıyor: her komutun
    `FlagSet` kurucusu ayrı bir işleve alınır, `run…` de aynı kurucuyu kullanır.
- **Bağlantı (benim kararım, 19 Kasım):** konsol, sunucu başına kendi kalıcı bağlantısını tutar;
  GUI'nin bağlantı havuzunu paylaşmaz.
  - Gerekçe: her CLI komutu aldığı bağlantıyı kapatıyor (`defer conn.Close()`, ör.
    `status.go:32`, `deploy.go:54`). Havuzun `connFor`'u dışa kapalı ve `PingResponse`'u atıyor
    (`internal/sidecar/dispatcher.go:282-316`), CLI ise `TotpRequired` için o ping'i istiyor
    (`main.go:261`). Paylaşılan bağlantıda oturum yenilemesi (`SetSession`) akan bir `logs -f` ile
    yarışabilir.
  - Konsolun `Dial`'ı ilk komutta bağlanır. Komuta, `Close`'u bağlantıyı kapatmayan bir
    sarmalayıcı verir. Panelde aynı anda tek komut çalıştığı için oturum yarışı olmaz.
  - Saklanan bağlantıdır, ping değil: her komutta açık bağlantı üstünden yeniden ping atılır.
    `TotpRequired` aynı konsol oturumunda `totp enable|disable|reset` ile değişir ve
    `runTotpEnable` buna göre dallanır; saklanan ping yanlış dala götürürdü. Açık bağlantıda
    ping ucuzdur; ~1 sn'lik bedel SSH el sıkışmasıydı.
  - Sunucu değişince (`use`) ya da bağlantı koparsa bir sonraki komut yeniden bağlanır.
  - Bedeli: konsol açıkken sunucu başına bir SSH bağlantısı daha.
- **Kabul:** CLI'ın davranışı değişmez. Bugünkü `cmd/kadran` testleri paketle taşınır ve
  aynen geçer; mutasyon betikleri yeni yolu bulur; `kadran` ikilisinin yeniden üretilebilir
  derlemesi (K-112) ve `version` çıktısı aynı kalır. Konsoldaki `version` masaüstünün sürümünü
  yazar; sunucu paketi satırları orada yoktur (masaüstü paketi taşımaz) **(benim kararım)**.

### 2. Komut sınıfları

Her komut (ve alt komut) tam olarak bir sınıfa girer; tablo `internal/cli`'da, komut
tanımının yanında **(benim kararım)**. Bir test, `Commands()`'taki her komutun sınıflandığını
denetler; sınıfı olmayan komut konsolda reddedilir (kapalı varsayılan).

| Sınıf | Komutlar | Konsolda |
|---|---|---|
| okuma | `status`, `app list`, `app show`, `logs`, `alarms`, `backup list`, `domain check`, `audit list`, `audit verify` (`-anchors` olmadan), `github status`, `totp status`, `server list`, `version`, `help` | doğrudan |
| oturum | `login`, `logout` | doğrudan; `login` kodu konsol girdisinden okur |
| değiştirme | `app create`, `app update`, `deploy`, `backup create`, `prune -dry-run`, `github connect` | yerel onay kutusu |
| tehlikeli | `app delete`, `rollback`, `prune` (dry-run değilse), `github disconnect`, `totp enable`, `totp disable` | uygulama/sunucu adını yazma + yerel onay kutusu; `totp enable`'ın gizli çıktısı kartta (6a) |
| root | `key list`, `key add`, `key remove`, `totp reset` | ayar açık + sunucu adını yazma + yerel onay kutusu + yerel kayıt |
| yasak | `sidecar`, `server add`, `server rm`, `server rename`, `bootstrap`, `audit verify -anchors` | reddedilir, nedeni yazılır (`bootstrap` ve `-anchors` için "CLI'da çalıştır") |

Not: `prune -dry-run` hiçbir şey silmez ama listeleme bir değiştirme komutunun kipi olduğu için
onaylı sınıfta tutuldu **(benim kararım; uygulayıcı okuma sınıfına almak isterse gerekçesini
K-169'a yazar)**. `key list` okuma ama root yolundan gider: root sınıfında.

### 3. Masaüstü köprüsü ve K-165 istisnası

K-165: köprü bir beyaz listedir; arayüze "şu yöntemi çağır" türü genel kapı verilmez. Konsolun
satır çalıştıran yöntemi böyle bir kapıdır. **Bilinçli istisna** (K-169'a yazılır):

- Kapının arkasındaki her komut zaten tipli ve sunucuda sınırlı (`kadran-client` yalnız RPC
  beyaz listesine ulaşır; root yolu ayrı katmanlarda).
- Sınıflandırma ve onay **Go tarafında**, arayüzden önce yapılır; arayüz sınıfı ya da onayı
  atlayamaz. Durum değiştiren her komut, aynı işlemin GUI düğmesiyle aynı yerel onay kutusundan
  geçer (`internal/confirm`, varsayılan düğme İptal, aynı anda tek kutu).

Yeni köprü yöntemleri **(benim kararım: adlar uygulayıcının)**:

- `ConsoleRun(server, line)` → çalıştırma kimliği. Satır Go'da ayrıştırılır (aşağıda), komutun
  kendi `FlagSet` kurucusuyla bayrakları çözülür, sınıfa göre onaylanır, sonra
  `internal/cli.Run` ayrı bir goroutine'de çalışır. Onay `Run`'dan önce kurulur: CLI'ın kod
  yolu değişmez **(benim kararım, 19 Kasım)**.
- `ConsoleSetAdmin(on)` → "konsolda yönetici komutları" ayarı. Açmak Go tarafında yerel onay
  kutusu ister; ayar kendi dosyasında durur, görünüm deposunda (`appearance.json`) değil.
  Gerekçe: arayüz `SaveAppearance(raw)` ile görünüm dosyasını yazabiliyor
  (`desktop/internal/bridge/bridge.go:174-177`) ve K-165 arayüze güvenmez. Kapatmak onaysızdır
  **(benim kararım, 19 Kasım)**.
- `ConsoleInput(runID, text)` → çalışan komutun istemine girdi (TOTP kodu, yazılı onay).
  Girdi geçmişe ve hiçbir günlüğe yazılmaz.
- `ConsoleCancel(runID)` → Ctrl+C; komutun bağlamı iptal edilir.
- `ConsoleComplete(server, line, cursor)` → aday listesi.
- Çıktı Wails olaylarıyla akar (`runtime.EventsEmit`; üretimde bugün kullanılmıyor, yeni):
  çalıştırma kimliği, akış (stdout/stderr), veri; bitişte çıkış kodu. Olaylar ~50 ms'de bir
  birleştirilir **(benim kararım)**.
- Bir panelde aynı anda tek komut çalışır **(benim kararım)**.
- `bridge_test.go`'daki beyaz liste testi yeni yöntemlerle güncellenir.

**Satır ayrıştırma (Go):** boşlukla ayrılmış sözcükler; çift ve tek tırnak; kaçış yok, genişletme
yok. Denetim (Cc) ve biçim (Cf, bidi) karakterleri reddedilir (`guard`'daki kural). Satır en çok
4096 bayt **(benim kararım)**. Satırda `|`, `>`, `<`, `;`, `&`, `` ` ``, `$(` tırnak dışında
geçerse "konsol kabuk değildir" hatası **(benim kararım: kullanıcının bunları kabuk sanıp yanlış
bir şey çalıştırmasını önler)**.

### 4. Sunucu bağlamı ve konsola özgü komutlar

- Sunucu seçimi tek **(kullanıcı onayladı)**: istem satırı kenar çubuğunda seçili sunucuyu
  gösterir (`hetzner ›`); konsolda `use gcp` kenar çubuğunu da gcp'ye geçirir.
- Konsol her komuta seçili sunucuyu `-s <ad>` olarak ekler. Kullanıcı satıra başka bir sunucu
  için `-s` ya da bir hedef (`kullanıcı@host`) yazarsa komut çalışmaz: "sunucu değiştirmek için
  `use <ad>`" **(benim kararım: yanlış sunucuya gitme sınıfını kapatır)**. `key` ve `totp reset`
  için kayıttaki yönetici hedefi (K-162'deki `-s` genişletmesi) kullanılır.
- **CLI'ın ipuçları konsolda da çalışır (benim kararım, 19 Kasım).** CLI çıktısı birebir aynı
  kaldığı için `kadran login -s hetzner` gibi ipuçları ve bayrak yardımındaki `-s name` satırları
  konsolda da görünür. Kopyalanınca çalışsınlar diye:
  - satır başındaki tek `kadran` sözcüğü atılır;
  - seçili sunucuyla aynı adı taşıyan `-s` kabul edilir ve etkisizdir.
  Farklı bir `-s` yine reddedilir. `server add`'e yönlendiren ipucu (`noAdminHint`) konsolda
  da görünür; `server add` yasak olduğu için konsol "sunucu ekleme arayüzdeki akışta" der.
- Konsola özgü üç komut **(benim kararım)**: `use <sunucu>`, `clear` (Ctrl+L), `help` (CLI'ın
  komut listesi + konsol kısayolları). Başka yerleşik komut yok.
- Seçili sunucu yokken (boş liste) yalnız `help`, `use` ve `server list` çalışır.

### 5. Onay akışları

- **değiştirme:** yerel onay kutusu: sunucu, arıtılmış komut satırı, ne yapacağı. GUI'deki
  karşılığıyla aynı metin kalıbı (`confirm.Build`); karşılığı olmayan komutlar için yeni tür.
  Uygulamada konsol her komutta tek biçimi (`confirm.BuildConsole`) kullanır: bayraklardan
  Spec/Update'i yeniden kurmak CLI'ın bayrak → istek çevirisini ikinci kez yazmak olurdu. Bedeli:
  aynı işlemin kutusu GUI'de ve konsolda farklı görünür (R16) (uygulamada değişti, K-169 Uygulama).
  Kutuda her alan etiketiyle başlar (etki de); sarılan alanın devam satırları dört boşlukla
  girintilidir, kullanıcı değeri etiket satırı gibi görünemez.
  **Komut satırı kutuda HİÇ kesilmez:** satırın tamamı (kaçışlı) satırlara bölünerek gösterilir;
  kaçışlı arıtılmış satır 1024 UTF-16 birimini aşarsa kutu hiç açılmaz, çıkış 1 ile "too long to
  confirm here; run it in the CLI" denir. Gerekçe: değiştirme sınıfında tek kapı bu kutudur ve
  bayraklarda son yazılan kazanır; ortadan kesilen satırda ikinci bir `-domain` görünmeden
  onaylanırdı (uygulamada değişti, K-169 Uygulama).
  Kutudaki bilgiler, `Run`'dan önce komutun kendi `FlagSet` kurucusuyla çözülen bayraklardan
  gelir **(benim kararım, 19 Kasım)**. Ayrıntılar:
  - `-env` ve `-build-arg` değerleri kutuda gizlidir, GUI'deki gibi (`desktop/internal/confirm/text.go:53`);
    yalnız anahtar adları görünür.
  - `deploy`'da kutu, verilen dalı ya da commit'i gösterir. Dalın hangi commit'e çözüleceği
    bağlandıktan sonra belli olur (`deploy.go:57`); kutu bunu "çözülecek" diye yazar.
  - İş istasyonundan dosya okuyan bayrakların yolu kutuda tam görünür (`key add <dosya>`).
- **tehlikeli:** önce konsolda "onaylamak için `<uygulama ya da sunucu adı>` yaz" istemi
  (`ConsoleInput` ile), sonra yerel kutu. GUI'de `AppDelete`/`AppRollback`'in yazılı onayıyla
  aynı kural. CLI'ın kendisinde bu onay yok; ekleme konsol katmanında **(benim kararım: CLI'ın
  davranışı değişmez)**.
- **root:**
  1. Ayarlarda "konsolda yönetici komutları" kapalıysa (varsayılan **kapalı**, kullanıcı
     onayladı) komut çalışmaz; konsol ayarın yerini söyler. Ayar yalnız `ConsoleSetAdmin` ile,
     yerel onay kutusundan geçerek açılır (Tasarım 3).
  2. Sunucu adını yazarak onay. Yazılan ad arayüzden gelir. Bu adım kullanıcının dikkatini
     çeker ama güvenlik kapısı değildir; kapı, 3. adımdaki yerel kutudur.
  3. Yerel onay kutusu: sunucu, yönetici hedefi (`root@…` ya da `-sudo kullanıcı@…`), komut,
     sunucuda neyi değiştireceği.
  4. Komut kullanıcının kendi OpenSSH'ı ve ajanıyla çalışır (CLI'daki `exec ssh` yolu); masaüstü
     anahtar ya da parola saklamaz, bağlantı komutla biter. CLI'dan iki farkı var, ikisi de
     `Env`'deki `ssh` kancasından: konsol penceresi gizlenir ve `-o StrictHostKeyChecking=yes`
     eklenir (aşağıda).
  5. Çalışırken istem kırmızı ve `#` (`hetzner #`).
  6. Yerel kayıt: kullanıcının yapılandırma dizininde `console-admin.log`, satır başına bir JSON:
     zaman (UTC), sunucu, komut (arıtılmış), çıkış kodu, süre. Yalnız ekleme; dosya izinleri
     kullanıcıya özel **(benim kararım: yer ve biçim)**.
- **İlk kez görülen host anahtarı:** root komutu, host anahtarı `known_hosts`'ta olmayan bir
  sunucuya gitmez; "önce CLI'da bir kez bağlan" der **(benim kararım: host anahtarı onayını
  konsol içinden almak, ilk bağlantıda araya girme riskini GUI'ye taşır)**. Uygulama
  (19 Kasım): root yolundaki SSH çağrıları zaten `BatchMode=yes` ile çalışıyor
  (`internal/bootstrap/bootstrap.go:477`, `internal/client/client.go:402`). Bu, yalnız
  kullanıcının `ssh_config`'i `StrictHostKeyChecking=accept-new` ya da `no` demiyorsa yeter.
  Konsol, root yolunda `-o StrictHostKeyChecking=yes`'i açıkça ekler. Ayrı bir `known_hosts`
  okuması gerekmez; bilinmeyen host'ta SSH'ın hatası yukarıdaki mesaja çevrilir.

### 6. Çıktı

- CLI'ın stdout ve stderr metni olduğu gibi gösterilir **(kullanıcı onayladı)**. Çıktı satırları
  hiçbir zaman boyanmaz; stderr ikincil renkte kalır **(benim kararım)**. Komutun bitiş işareti
  çıktıya değil komutun yankı satırına konur: sıfırla biterse satırın başında yeşil `●`, sıfır
  dışıyla biterse kırmızı `●` ve satırın sonunda `· exit N`; işaret çizimde `exit` alanından üretilir, satır metnine
  yazılmaz **(R19; canlıda değişti, K-169)**.
- Panelde en çok 5.000 satır tutulur, eskiler düşer **(benim kararım: 300 MB bellek bütçesi,
  K-165)**. Panel kapanıp açılınca içerik kalır; uygulama kapanınca gider.
- Çıktı seçilip kopyalanabilir (bugünkü `CopyText` sınırıyla).

### 6a. Gizli çıktı kartı (`totp enable`)

`totp enable` yeni TOTP sırrını, `otpauth` adresini ve QR kodunu basar, bir kod ister, sonra
kurtarma kodlarını basar (`cmd/kadran/totp.go`, `runTotpEnable`, `printEnrollment`). Konsolda
bunlar normal çıktıya girseydi web görünümüne gider ve 5.000 satırlık tamponda kalırdı. Komut
konsolda kalır, gizli kısım ayrı bir kartta gösterilir **(kullanıcı, 19 Kasım: "kart olsun";
seçenekler konsoldan çıkarmak ve Windows'un kendi kutusuydu)**.

- **Kanal:** CLI'da sırrı, adresi, QR'ı ve kurtarma kodlarını basan yerler `Env.Sensitive`'e
  yazar. CLI'da bu `Stdout`'tur: CLI'ın çıktısı bayt bayt aynı kalır (taşıma testinin çıktı
  karşılaştırması bunu da kapsar).
- **Kart:** konsol `Sensitive`'i normal akış olaylarına katmaz, ayrı bir olayla panelin üstünde
  açılan karta gönderir. Kart metni olduğu gibi, eş aralıklı yazıyla ve satır aralığı 1 ile
  gösterir; CLI'ın metin QR'ı böylece taranabilir kalır **(benim kararım: QR'ı adresten yeniden
  üretmek, metni ayrıştırmayı gerektirirdi)**. Kod istemi beklerken kart açık kalır, kurtarma
  kodları gelince aynı karta eklenir.
- **Hiçbir yere yazılmaz:** kart içeriği çıktı tamponuna, geçmişe, `console-admin.log`'a ve hiçbir
  günlüğe girmez; seçilip kopyalanamaz. Konsol satırlarında yerinde yalnız "gizli çıktı ayrı
  kartta gösterildi" yazar.
- **Silinir:** "Kaydettim" düğmesi ya da panelin kapanması kartı kapatır. Kart kapanınca içerik
  ekrandan ve DOM'dan kalkar, Go tarafı tuttuğu kopyayı bırakır. Komut sıfır dışı bir kodla
  biterse kart hemen kapanır. Komut 0 ile bitince kart AÇIK KALIR: `totp enable` kurtarma
  kodlarını en sonda basıp hemen bittiği için, bitişte kapanan bir kart kodları hiç göstermezdi
  (R14) (uygulamada değişti, K-169 Uygulama).
- **Kart çalışmasına bağlıdır (R17):** kart onu üreten çalışmanın kimliğini taşır; yalnız o
  çalışma sıfır dışı biterse kapanır. Sonraki bir komutun hatası (yazım hatası, alarm varken
  `alarms`) bir kez gösterilen kurtarma kodlarını silmez. Kaydedilmemiş kart açıkken yeni satır
  çalışmaz; istem "Önce gizli kartı kapatın (Kaydettim)." der. Ctrl+` ve panelin kapanması
  çalışır (panel kapanınca kart da kapanır). Bedeli: kart kapanana kadar konsol bekler
  (uygulamada değişti, K-169 Uygulama).
- **Kalan risk:** sır, kart açıkken web görünümünün belleğinde ve ekrandadır. Bu, sırrın
  terminal ekranında durmasıyla aynı risk; terminalin kaydırma geçmişindeki kalıcılık burada yok.
- Bu kanalı yalnız `totp enable` kullanır. Başka bir komut gizli değer basmaya başlarsa aynı kanala
  alınır; sınıflandırma testi `Sensitive` kullanan komutların listesini sabitler.

### 7. Geçmiş ve tamamlama

- ↑/↓ ile geçmiş, sunucu başına en çok 500 satır, uygulama kapanınca da kalır **(kullanıcı
  onayladı)**. Dosya kullanıcının yapılandırma dizininde.
- Yazılmadan önce arıtılır: `-env` değerleri `-env KEY=***` olur (Go'nun kabul ettiği her
  biçimde: `-env K=V`, `-env=K=V`, `--env K=V`, tekrarlı);
  `login` gibi istemlere girilen hiçbir şey geçmişe girmez (girdi satır değil). Arıtma tek bir
  işlevde, mutasyonla korunur.
- Tab ile tamamlama **(kullanıcı onayladı)**: komut ve alt komut adları, bayraklar
  (`Commands()`'tan), sunucudaki uygulama adları (son `AppsList`'ten, en çok 30 sn bayat),
  `use` için kayıtlı sunucu adları.

### 8. Uzun süren komutlar

- `logs -f` ve `deploy` çıktıyı canlı akıtır **(kullanıcı onayladı)**. Ctrl+C `ConsoleCancel`
  çağırır; CLI'daki Ctrl+C davranışıyla aynı (bağlam iptali; `deploy`'da sunucu sürümü mühürler).
- Komut çalışırken panel girdi satırı yalnız komutun istemi varsa açıktır; yoksa satır kilitli
  ve "çalışıyor · Ctrl+C ile durdur" yazar.

### 9. Panel

- Her ekranın altında (Sunucular, Uygulamalar, sunucu sayfaları); Ctrl+` ile açılır ve kapanır
  **(kullanıcı; her ekran: R20, canlıda değişti, K-169)**. Konsolun seçili sunucusu GUI'nin
  bağlı sunucusundan ayrı tutulur; bağlantısı kurulamayan ya da oturumu düşmüş sunucu da `use`
  ile seçilebilir, böylece `login` konsoldan yapılır **(R20; canlıda değişti, K-169)**.
- Yükseklik sürüklenerek ayarlanır (en az 120 px, en çok pencerenin %70'i); açık/kapalı durumu
  ve yükseklik sonraki açılışa kalır **(kullanıcı onayladı; sınırlar benim kararım)**. Mevcut
  görünüm deposuna (`appearance.Store`) eklenir.
- Üç temada (koyu, açık, cam) aynı token'larla; yazı paketli `--font-mono` (Geist Mono; JetBrains
  Mono eklenmedi: yeni paketli yazı tipi yeni dosya ve lisans demek, R15) (uygulamada değişti, K-169 Uygulama); vurgu buz mavisi;
  köşeler 4 px. İstem rengi: normalde vurgu, root'ta tehlike rengi.
- Kısayollar: Enter çalıştır, ↑/↓ geçmiş, Tab tamamla, Ctrl+C durdur, Ctrl+L temizle, Esc
  paneli kapatmaz (yalnız Ctrl+`) **(benim kararım)**.

### 10. Sınırlar

- Ayrıcalıklı yüzey (`kadran-exec`) değişmez: konsol tamamen istemci tarafı.
- Sunucu tarafı değişmez; protokol 3 kalır.
- Boşta özel bellek bütçesi 300 MB (K-165); konsol açıkken ve 5.000 satır doluyken yeniden ölçülür.

## Test ve kabul

- `internal/cli` taşıması: bugünkü CLI testleri aynen geçer; çıktı karşılaştırması (taşıma
  öncesi ve sonrası aynı komutların stdout/stderr'i) değişiklik göstermez.
- Sınıflandırma: her komut sınıflı; sınıfsız komut reddedilir; her sınıfın onay yolu ayrı test.
- Satır ayrıştırıcı: tırnaklar, denetim/bidi karakterleri, kabuk işaretleri, uzunluk sınırı;
  baştaki `kadran` sözcüğünün atılması; seçili sunucuyla aynı `-s`'in kabulü, farklı `-s`'in ve
  hedefin reddi.
- Arıtma: `-env` değerleri her bayrak biçiminde (geçmişte ve onay kutusunda); istem girdisinin
  geçmişe hiç girmemesi.
- Köprü: beyaz liste testi; onaysız durum değiştirmenin imkânsızlığı (sahte Host ile); root
  ayarı kapalıyken reddedilme; ayarın görünüm dosyasından ve onaysız açılamaması; `bootstrap`
  ve `audit verify -anchors`'ın reddi.
- Gizli kanal: `totp enable` sahte sunucuya karşı konsolda koşar; sır, adres ve kurtarma kodları
  çıktı tamponunda, geçmişte, yönetici kaydında ve akış olaylarında hiç geçmez, yalnız kart
  olayında geçer. CLI'da aynı komutun stdout'u taşıma öncesiyle aynıdır.
- Bağlantı: aynı konsol oturumunda `totp enable` sonra `totp status` ve ikinci bir
  `totp enable` doğru dala gider (ping her komutta yenilenir).
- Mutasyon: sınıflandırma, arıtma, onay kapısı, root ayarı ve gizli kanalın yönlendirmesi
  mutasyon betiğine eklenir.
- Duman (masaüstü): bugünkü duman derlemesi JSON-RPC sidecar'ını taklit ediyor, gRPC'yi değil
  (`desktop/dispatcher_smoke.go:19-21`). Konsol CLI'ın gRPC istemcisini çalıştırdığı için duman
  testine sahte bir `Dial` gerekir: süreç içinde, `bufconn` üstünde, gereken RPC'leri yanıtlayan
  sahte bir kadrand **(benim kararım, 19 Kasım; biçimi uygulayıcının)**. Senaryolar:
  - `apps`;
  - `deploy`, onay kutusu sahte Host'ta;
  - `logs -f` ve iptal;
  - `totp enable` kartı ve kartın kapanınca DOM'dan kalkması;
  - root komutunun ayar kapalıyken reddi.
- Elle (Windows): konsoldan `domain check` ve bir root komutu çalışırken konsol penceresi
  açılmıyor (`ssh` kancası).
- Elle canlı: iki sunucuda `login`, `apps`, `deploy`, `logs -f`, `rollback` (yazılı onay), bir
  root komutu (`key list`) ayar açıkken; bellek ölçümü.

## Uygulayıcıya kalanlar

- Paket, olay ve dosya adları; `Env` alanlarının tam listesi.
- `FlagSet` kurucularının ayrılma biçimi.
- Olay birleştirme süresi ve satır sınırı (gerekçesiyle değiştirilebilir).
- Onay kutusu metinlerinin tr/en karşılıkları (`internal/confirm`'ün kalıbıyla).
- Panelin React bileşen yapısı (mevcut `desktop/src/renderer` düzeniyle).

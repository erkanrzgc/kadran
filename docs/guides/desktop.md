# Masaüstü kullanım rehberi

Kadran masaüstü, SSH üzerinden kendi sunucunu yönetir. Uygulama özel SSH anahtarını veya Git token'ını okumaz ve saklamaz. Anahtar yönetimi OpenSSH/ssh-agent, sunucu kurulumu `kadran bootstrap` üzerinden yapılır.

## Derleme

Masaüstü sürüm dosyalarında yok; kaynaktan derlenir. Gerekenler: Go 1.26.10+, Node 22.6+, [`buf`](https://buf.build/docs/installation) ve `buf.gen.yaml`'daki iki yerel eklenti. Depo kökünde `buf generate`, sonra `desktop/` içinde `npm ci` ve `npm run dist`. Paket bu makinenin platformu için `desktop/dist/Kadran-<platform>-x64/` altına çıkar ve imzasızdır. Kenar çubuğundaki sürüm, pakete gömülen `kadran` ikilisinin sürümüdür.

## İlk bağlantı

1. Sunucuyu README'deki kurulumla hazırla; yönetici açık anahtarın `kadran-client` kullanıcısına kaydedilmiş olsun.
2. Bu bilgisayarda OpenSSH kurulu olmalı. Parola korumalı anahtarın varsa önce ssh-agent'a ekle.
3. Sunucunun SSH host anahtarını sağlayıcı konsolu veya güvenilir bir kanaldan doğrula, sonra OpenSSH'nin bilinen sunucu kaydını hazırla. Masaüstü `BatchMode=yes` kullanır: ilk güven veya parola istemi açmaz.
4. **Sunucular** ekranında **Sunucu ekle**'ye bas, `kadran-client@sunucu` veya `kadran-client@sunucu:2222` hedefini yaz. Profil adı bu bilgisayarda saklanır. Hata metni kimlik, host güveni veya ağ sorununu ayırt etmek için korunur.

Sunucu listesi CLI ile ortaktır (K-162): `kadran server add` ile eklenen sunucu, pencereye dönünce kart olarak görünür; masaüstünde eklenen de `kadran status -s <ad>` ile kullanılır. Liste yapılandırma dizininde durur (`%AppData%\kadran\servers.json`, Linux'ta `~/.config/kadran/servers.json`) ve anahtar, parola ya da jeton taşımaz. Bu sürüme ilk geçişte masaüstünün eski listesi bir kez bu dosyaya taşınır; eski `profiles.json` silinmez. Taşıma başarısız olursa uygulama söyler ve bir sonraki açılışta yeniden dener.

Uygulama **Sunucular** ekranında açılır: kayıtlı her sunucu bir kart. Karta basınca sunucu açılır; üst çubuktaki `Sunucular / ad ⌄` menüsünden başka sunucuya geçilir, iki ya da daha çok sunucu açıkken üst çubukta sekmeler çıkar. Eski sunucunun geç gelen yanıtı yeni sunucunun ekranını değiştirmez. Başlatılmış bir işlem, ekranı kapatınca otomatik olarak geri alınmış sayılmaz; yeniden bağlanıp sürüm geçmişini kontrol et.

## Uygulamalar

**Applications / Uygulamalar** altında uygulama oluştur, kaynak deposunu ve Dockerfile'ı seç; port, sağlık yolu, alan adı, replika ve zorunlu kaynak sınırlarını belirle. Kaynak ve konteyner portu oluşturulduktan sonra bu formdan değişmez; ayrı uygulama tanımı gerektirir.

Uygulama ayrıntısında canlı sürüm ile derleme durumu ayrı gösterilir. “Built” bir imajın üretildiğini söyler; “Live” trafiğin o sürüme yönlendirildiğini söyler. Eski sunucu canlı sürüm bilgisini sağlamazsa arayüz “bilinmiyor” gösterir.

**Deploy / Dağıt** dal, etiket veya tam 40 haneli küçük harf commit kabul eder. Dal çözümü bu bilgisayarda yapılır; sunucuya sabit commit gider. Özel depoda tam commit kullan. İşlem sırasında düğmeler kilitlenir; çıktı en fazla 512 KiB tutulur. Zaman aşımı kesin geri alma anlamına gelmez: yeniden dağıtmadan önce geçmişi yenile. **Rollback / Geri al** önceki aktif sürüme döner; keyfi bir eski sürüm seçimi desteklenmez.

Dağıtım, geri alma ve silme penceresinde uygulamanın adını birebir yaz. Durum değiştiren HER işlem (oluşturma, değiştirme, dağıtım, geri alma, silme, yedek) ayrıca ana süreçte yerel onay ister; varsayılan düğme Vazgeç'tir ve aynı anda tek onay kutusu açılır. Kutu sunucuyu, uygulamayı ve neyin değişeceğini gösterir; ortam değişkenlerinin ve build argümanlarının yalnız adları görünür, değerleri asla. Kutuda vazgeçersen işlem penceresi kapanır ve ekranda "vazgeçildi" iletisi kalır. Canlı sürümü olan uygulama silinemez. Silme kalıcı hacim verisini silmez; hacimlerin sunucuda kaldığını sonuç belirtir.

## Yapılandırma ve veri

Ortam değişkenlerinin mevcut değerleri sunucudan geri okunmaz. Bir alan adını değiştirirken gizli değerlerin korunması gerekir; düzenleyicide yalnız açıkça değiştirdiğin değerler gönderilir. Yeni değerler age ile mühürlenir, ancak çalışan konteyner ortamında düz metin bulunur. Dockerfile çıktısına veya build argümanlarına sır yazma.

Hacimler ada göre birleştirilir; bağlamayı kaldırmak diskteki veriyi silmez. Replikalar ve eski/yeni sürüm aynı dizini kullanır. SQLite veya tek yazarlı veritabanını blue-green sırasında iki sürüme açma. Harici veritabanı ya da uygulamaya özel bakım prosedürü kullan.

## Log, alarm, yedek ve denetim

- **Logs / Loglar** son 100, 500 veya 1000 satırı getirir. Sürekli akış değildir. Yenile düğmesi yeni bir görüntü alır; çıktı boyut sınırına ulaşınca kesildiği belirtilir.
- **Alarms / Alarmlar** etkin arızaları gösterir. Yerel liste dışarı bildirim teslimini kanıtlamaz; Telegram isteğe bağlı ayrı kurulumdur.
- **Backups / Yedekler** kontrol veritabanı yedeklerini listeler ve yeni yedek alır. Bunlar uygulama hacimlerini içermez. Kurtarma için [operasyon rehberini](operations.md) kullan.
- **Audit / Denetim** kayıtların aktörünü, parametrelerini ve sonucunu ayrıntıda gösterir. Doğrulama başarısızsa eski yeşil sonuç güncel gösterilmez. Son başarılı zaman görünür; sonuç beş dakika sonra eski sayılır. Önceden gözlenen bozuk zincir kanıtı hata ile silinmez.

## Görünüm

**Settings / Ayarlar** altında üç tema var: **Koyu**, **Açık** ve **Cam**. Cam varsayılandır ve pencerenin arkasındaki masaüstünü bulanık gösterir. Windows 11'de pencere arka plana geçince de cam kalır; bunu sağlayan Windows çağrısı yüklenemezse pencere arka planda koyulaşır. Yerel bulanıklığı olmayan sistemde cam seçeneği görünmez ve pencere koyu açılır. Dil ve yazı tipleri de buradadır; ayarlar bu bilgisayarda kalır.

En küçük desteklenen pencere 760×520'dir. Küçük pencerede içerik dikey kayar; formlar ve kartlar yeniden düzenlenir. Görünüm yalnız Windows 11'de gerçek pencerede doğrulandı (K-161); macOS, Linux ve Windows 10 ölçülmedi.

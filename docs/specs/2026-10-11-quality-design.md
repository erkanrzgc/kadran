# Kadran güvenilirlik ve cam yönetim tasarımı

Başlangıç: `90a6c55` (`masaustu-temel`). Kullanıcının 11 Ekim 2026 isteği önceki inceleme bulgularını gidermek, mimariyi ve kullanım kolaylığını geliştirmek, Coolify'ın kaynak odaklı düzeninden esinlenen gerçek saydam cam masaüstü sunmaktır.

## Kabul ölçütleri

Puan hedefi bağımsız bir değerlendirme gerektirir; bu çalışma kendi kendisine 8,5 puan vermek yerine aşağıdaki davranışları doğrular.

1. Daemon ele geçirilse bile ham Caddy yapılandırması veya dosya okuyan placeholder Caddy'ye ulaşmaz. Ayrı bir süreçte sınırlı vekil arayüzü, Unix dosya izinleri ve bilinmeyen alanları reddeden doğrulama birlikte sınırı kurar. Daemon Docker erişimi kazanmaz.
2. Aynı uygulamanın dağıtım, geri alma, güncelleme, silme, iyileştirme ve budama işleri ortak iptal edilebilir sıra kullanır. Farklı uygulamalar bağımsız çalışır. Kendi rotası atlanan işlem başarısızdır. HTTP yanıt gövdesi zaman aşımı sağlıklı sayılmaz. Vekil sapması otomatik onarılır.
3. Masaüstü uygulama listesi, tanım oluşturma, uygulama/sürüm ayrıntısı, dağıtım ve geri alma, sınırlı log okuma, alarmlar ve kontrol veritabanı yedeklerini sağlar. İşlemler açık adlar, bekleme durumu ve onay ile sunulur; başarısız iş başarı rengine dönüşmez.
4. Yönetim köprüsü yönteme özel IPC kanalları ve doğrulanan parametreler kullanır. Serbest kabuk/argv, anahtar veya token saklama ve Docker soketi eklenmez. Yeni isteklerin yetkilendirmesi mevcut sunucu API'sinde kalır. Uzun dağıtımların süre sınırı okuma çağrılarından ayrıdır.
5. Coolify'dan esinlenen sol navigasyon, kaynak kartları, uygulama ayrıntı sekmeleri ve görünür operasyon durumları; Kadran'a özgü buz rengi vurgu ve saydam cam yüzeyler. Windows Acrylic/Clear ve macOS Vibrancy korunur. Saydamlık ayarlanabilir, 760x520 ve büyük pencerelerde yatay taşma olmaz. Koyu/açık ve Türkçe/İngilizce destek korunur.
6. Başarısız zincir doğrulaması eski yeşil sonucu güncel göstermez; son doğrulama zamanı ve hata durumu görünür. UTF-8 parçaları doğru birleştirilir, geçerli JSON'un yanlış şekli uygulamayı çökertmez. Audit kaydı ayrıntıları erişilebilir.
7. İlk bağlantı ekranı sunucu hedefi, OpenSSH, anahtar/agent ve ilk host güvenini anlaşılır açıklar; gerçek hata korunur. Temiz kurulum, yükseltme, günlük kullanım ve veri kurtarma rehberleri tek girişten bulunur.
8. Özel Git deposu desteği sunucu yetki sınırını koruyan ayrı bir tasarım/test kapısıyla ele alınır. Kimlik bilgileri renderer/daemon/denetim kaydı içine alınmaz; gerçek token veya canlı sunucu olmadan yapılmış doğrulama bu sınırla raporlanır.

## Tasarım kararları

Mevcut Go + Electron + React uygulaması genişletilir. Web paneli veya Coolify'ın kodu eklenmez. Baştan yazım mevcut test ve protokol yatırımını kaybeder; yalnız görsel tema değişimi ise operasyon eksiklerini gidermez. Seçilen yaklaşım mevcut API'yi dar köprüyle erişilebilir yapmak ve eksik güvenlik sınırını ayrı süreçle kapatmaktır.

Vekil aracı Caddy'nin kendi kimliğiyle anahtar okumayı engelleyen bir dosya izni iddiasına dayanmaz. Daemon ham admin soketine erişemez; aracı yalnız sabit admin, sabit HTTP portları ve doğrulanmış domain/upstream kümelerinden yapılandırma üretir. Caddy'ye istek atan süreç ayrı kullanıcıdır ve executor soketine erişemez. Kurulum ve yükseltme izin değişimini ölçer.

Yaşam döngüsü kuyruğu uygulama anahtarına göre ayrılır; kuyrukta bekleyen iptal edilen çağrı işlem yapmaz. API işlemleri ile sağlık gözetmeni aynı koordinatörü paylaşır. Kuyruk sınırı bir tek rota yüklemesi yerine uygulamanın tüm değişimidir; iç içe kilit edinilmez.

Masaüstü kaynak yönetimi sınırlı JSON-RPC yöntemleriyle mevcut gRPC API'sine gider. Dağıtım olumlu son olay olmadan başarı sayılmaz. Loglar ve derleme çıktısı boyut/süre ile sınırlandırılır. Başarısız bağlantı eski ekranı güncel göstermez. Doğrulanmayan alanlar sentetik başarıyla doldurulmaz.

Cam tasarımda saydam pencere ile içerik okunurluğu birlikte korunur. Sol panel geniş ekranda sabit, dar ekranda küçülür; kartlar ve formlar yeniden akar. Onay kutuları modal/fokus yönetimi ile erişilebilir olur. Animasyonlar azaltılmış hareket tercihine uyar.

## Doğrulama ve tamamlanma kaydı

Önce yeniden üreten regresyon testleri, ardından düzeltme uygulanır. Go yarış testleri, buf/statik kontroller, masaüstü tür kontrolü/birim/derlenmiş çıktı testleri, gerçek Electron sentetik yönetim akışı, küçük pencere ölçümleri ve paketleme birlikte çalıştırılır. Caddy ayrı modülünün güvenlik testleri CI'a ayrıca eklenir. Canlı kurulum veya gerçek özel depo doğrulaması yapılmadıysa tamamlanma raporu bunu açıkça yazar.

Tamamlanan davranışlar `CHANGELOG.md`, `README.md`, `SECURITY.md`, `docs/decisions.md` ve çalışma raporunda yazılır; planlanmış olanlar uygulanmış gibi sunulmaz. Değişiklikler başlangıç commit'ine göre incelenir ve kullanıcının mevcut çalışması korunur.

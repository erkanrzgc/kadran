# Panely - Akıllı Sunucu Yönetimi ve Dağıtım Mimarisi Şartnamesi (Nihai Master Spec)

Bu doküman; tekil/çoklu Linux sunucuları (Hetzner VPS vb.), ağ trafiği, veritabanları, güvenlik katmanları, yerel yapay zeka modelleri ve felaket senaryolarının uçtan uca yönetilmesi amacıyla geliştirilecek **Panely** platformunun en kapsayıcı teknik ve mimari şartnamesidir.

---

## 1. Çekirdek Mimari, Sistem İzolasyonu ve Güvenlik

### 1.1. İzole Yetki ve Sıfır Güven (Zero Trust) Mimarisi
* **Kısıtlı Yetki Modeli (Least Privilege):** Panely ana servisi kesinlikle `root` yetkisiyle çalıştırılmaz. İşlemler, yalnızca tanımlı sistem çağrılarına (sys-calls) erişimi olan izole bir sistem kullanıcısı (`panely_daemon`) üzerinden yürütülür.
* **Parametreli Komut İcrası:** Arka plan servisi, sunucuda serbest bash komutları çalıştırmak yerine yalnızca önceden parametrelendirilmiş, doğrulanmış ve beyaz listeye alınmış komut şablonlarını işleyebilir.
* **Sistem Çağrısı Filtreleme (seccomp):** Çekirdek seviyesinde süreçlerin yetkisiz bellek veya doğrudan donanım alanlarına erişimini engelleyen kısıtlama kuralları aktif tutulur.

### 1.2. Cloudflare Entegreli Güvenlik Düzenlemesi (WAF & Edge)
* **IP Gizleme (Origin Shielding):** Sunucunun gerçek IP adresi dış dünyaya kapatılır. Tüm gelen trafik Cloudflare Edge ağında karşılanarak bot taramaları, SYN flood ve L7 DDoS saldırıları engellenir.
* **Saldırı Engelleme ve Otomatik Ban:** Başarısız oturum açma, yüksek frekanslı istek veya taranan geçersiz uç noktalarda ilgili IP adresleri Cloudflare WAF API entegrasyonuyla doğrudan edge seviyesinde engellenir.
* **IP Beyaz Liste (Whitelisting):** Yönetim paneline ve kritik API uç noktalarına erişim yalnızca belirli IP bloklarına sınırlandırılabilir.

### 1.3. Kimlik Doğrulama ve Güvenli Oturum Yönetimi
* **Çok Faktörlü Doğrulama (2FA/TOTP):** Tüm giriş süreçlerinde zamana dayalı tek kullanımlık şifre algoritması zorunlu tutulur. Acil durum erişimleri için şifrelenmiş kurtarma anahtarları üretilir.
* **Gelişmiş Oturum Mimarisi:** İmzalanmış kısa ömürlü jetonlar kullanılır. Oturum çerezleri `HttpOnly`, `SameSite=Strict` ve `Secure` bayraklarıyla korunur.
* **Değiştirilemez Denetim İzleri (Audit Logs):** Panely üzerinde yapılan her yapılandırma değişikliği, dosya müdahalesi veya servis işlemi zaman damgası ve IP adresiyle değiştirilemez bir günlüğe kaydedilir.

---

## 2. Kapsayıcı ve Uygulama Yaşam Döngüsü (Dağıtım Motoru)

### 2.1. Uygulama Yayınlama ve Dağıtım
* **Kesintisiz Yayınlama (Zero-Downtime / Blue-Green Deployment):** Yeni kod sürümleri canlıya alınırken, mevcut çalışan uygulama kopyası yeni kopya tamamen sağlıklı yanıt verene kadar trafiği karşılamaya devam eder.
* **Sürüm Geçmişi ve Otomatik Geri Alma (Rollback):** Başarısız güncellemelerde veya uygulama içi kritik hatalarda, tek tıkla saniyeler içinde bir önceki çalışan imaja veya kod sürümüne dönüş sağlanır.
* **Sağlık Denetçisi (Health Checker):** Her uygulama için periyodik aralıklarla iç ağ üzerinden durum sorguları (HTTP/TCP) yapılır. Yanıt vermeyen uygulamalar otomatik olarak yeniden başlatılır.

### 2.2. Kaynak İzolasyonu ve Çevre Yönetimi
* **Sıkı Kaynak Kotaları (cgroups):** Uygulama bazında maksimum RAM, CPU çekirdek oranı ve disk I/O sınırları tanımlanır. Sınırı aşan süreçler izole edilir.
* **Kasa (Vault) Mimarisi ile Gizli Değişken Yönetimi:** Veritabanı şifreleri ve API anahtarları veritabanında simetrik algoritmalarla şifrelenmiş olarak saklanır; yalnızca uygulama çalışma anında bellek üzerine aktarılır.
* **Tek Tıkla Servis Kataloğu (One-Click Apps):** İlişkisel veritabanları, önbellekleme servisleri veya mesaj kuyrukları tek hareketle izole ortamlarda yapılandırılıp ayağa kaldırılır.

---

## 3. Akıllı Ağ Katmanı, Trafik Yönetimi ve SSL

### 3.1. Ters Vekil (Reverse Proxy) ve Yük Dengeleme
* **Dinamik Yönlendirme:** Sisteme eklenen yeni domainler veya uygulamalar için ağ yönlendirme kuralları, ana proxy servisi yeniden başlatılmaya gerek kalmadan canlı olarak güncellenir.
* **Modern Protokol Desteği:** HTTP/HTTPS, WebSocket, gRPC ve HTTP/2 - HTTP/3 (QUIC) protokolleri proxy katmanında kesintisiz olarak iletilir.
* **Trafik Sınırlama (Rate Limiting):** Uygulama veya IP bazlı saniye/dakika başına istek limitleri tanımlanır.
* **Yük Dengeleme (Load Balancing):** Aynı uygulamanın birden fazla kopyasının çalıştırıldığı senaryolarda gelen istekler algoritmik olarak dağıtılır.

### 3.2. Sertifika Otomasyonu (SSL/TLS)
* **Otomatik Sertifika Tahsisi:** Yönlendirilen her alan adı için ACME veya Cloudflare Edge Certificate altyapısı üzerinden SSL sertifikası otomatik tanımlanır.
* **DNS-01 Challenge Desteği:** Wildcard (`*.domain.com`) sertifikalar için Cloudflare API entegrasyonu ile otomatik DNS doğrulama yapılır.
* **Güvenlik Başlığı Entegrasyonu:** HSTS, CORS ve CSP başlıkları arayüzden tek tıkla yapılandırılabilir.

---

## 4. Yerel Siber Güvenlik Yapay Zeka Modeli Entegrasyonu (LLM Engine)

* **Yerel LLM Servis Bağlantısı:** Sunucu üzerinde CPU/RAM üzerinden koşan yerel 9B siber güvenlik modeline (GGUF / llama.cpp / Ollama) iç ağ üzerinden erişim boru hattı.
* **Otomatik Log ve Anomali Analizi:** Panely canlı log akışında tespit edilen olağan dışı hataları veya şüpheli trafik hareketlerini yerel LLM modeline besleyerek anlık açıklama ve güvenlik önerisi üretme.
* **Güvenlik Kod ve Yapılandırma Denetimi:** Kullanıcı tarafından yüklenen Dockerfile, Nginx/Caddy veya ortam değişkeni dosyalarının yerel güvenlik modeli tarafından zafiyet analizi için taranması.

---

## 5. Gözlemlenebilirlik, İzleme ve Teşhis

### 5.1. Anlık Sistem Metrikleri ve Süreç Takibi
* **Çekirdek Düzeyinde Kaynak İzleme:** CPU yükü, RAM dağılımı, disk okuma/yazma hızı ve ağ bant genişliği anlık ölçülerek zaman serisi grafiklerine dönüştürülür.
* **Süreç Detaylandırması (Process Manager):** Yüksek kaynak tüketen süreçler izlenebilir ve panel üzerinden sonlandırılabilir.

### 5.2. Log Akışı ve Alarm Mekanizması
* **Canlı Log Akışı (Stream Pipeline):** Uygulamaların çıkış logları ve sistem kayıtları gecikmesiz olarak canlı akış ekranına yansıtılır.
* **Log Rotasyonu (Log Rotation):** Disk dolmasını önlemek amacıyla günlük dosyaları boyut limitlerine ulaştığında otomatik arşivlenir ve eski loglar temizlenir.
* **Eşik Tabanlı Alarm Sistemi:** Kritik kaynak kullanımı aşıldığında veya servis çöktüğünde dış sistemlere (Webhook, Telegram, E-posta) anında bildirim gönderilir.

---

## 6. CI/CD Otomasyonu, Derleme Motoru ve Görev Yöneticisi

### 6.1. Kod Deposu Entegrasyonu ve Otomatik Derleme
* **Depo Dinleyici (Webhook Receiver):** Kod deposundaki ana dala yeni bir commit gönderildiğinde Panely bunu anında algılar.
* **İzole Derleme Motoru:** Kod izole bir alana çekilir, derleme adımları çalıştırılır, imaj oluşturulur ve canlıya alınır.

### 6.2. Görsel Görev Zamanlayıcı (Cron Jobs)
* **Gelişmiş Cron Yöneticisi:** Periyodik görevler görsel bir arayüz üzerinden tanımlanır.
* **Çıktı ve Hata Arşivi:** Zamanlanmış her görevin çalışma süresi ve çıktıları saklanır.

---

## 7. Veritabanı, Depolama ve Güvenli Tünelleme

### 7.1. Veritabanı Orkestrasyonu ve Güvenli Tüneller
* **Erişim ve Yetki Matrisi:** Yeni veritabanı örnekleri oluşturulabilir, kullanıcılara okuma/yazma yetkileri dağıtılabilir.
* **Geçici Tünelleme (Bastion / Port-Forwarding):** Dış dünyaya kapalı veritabanlarına lokalden müdahale edebilmek için panel üzerinden geçici ve şifreli SSH tünelleri açma imkanı.

### 7.2. Kalıcı Depolama (Volume) Yönetimi
* **Veri Kalıcılığı:** Kapsayıcılar silinse bile verilerin kaybolmaması için kalıcı hacimler oluşturulur ve uygulamalara bağlanır.
* **Hacim Temizliği:** Kullanılmayan yetim (orphaned) hacimler otomatik tespit edilir.

---

## 8. Web Tabanlı Entegre Sistem Araçları

### 8.1. Güvenli Terminal Emülasyonu
* **Tarayıcı İçi Kabuk (PTY Bridge):** Panely üzerinden sunucuya veya izole ortamlara güvenli bir kabuk oturumu açılabilir.

### 8.2. Web Dosya Yöneticisi ve Kod Düzenleyici
* **Dosya Gezgini:** Sunucu dizinleri arasında gezinme, dosya yükleme, indirme ve sıkıştırma işlemleri yapılabilir.
* **İzin Yöneticisi & Editor:** Dosya izinleri (`chmod`/`chown`) değiştirilebilir; yapılandırma dosyaları sözdizimi vurgulamalı metin editörüyle düzenlenebilir.

---

## 9. Hibrit Bulut, Felaket Kurtarma ve Akıllı Yedekleme

### 9.1. Cloudflare R2 ile Off-Site Yedekleme (Sıfır Egress Ücreti)
* **3-2-1 Yedekleme Mimarisi:** Verilerin 1 kopyası tamamen bağımsız Cloudflare R2 nesne depolama alanında tutulur.
* **Sıfır Veri İndirme Maliyeti (Zero Egress Fees):** Cloudflare R2 sayesinde felaket anında terabaytlarca yedek geri yüklenirken bant genişliği ücreti ödenmez.
* **İstemci Tarafı Şifreleme (Client-Side Encryption):** Yedek verisi sunucudan çıkmadan simetrik anahtarlarla lokalde şifrelenir.

### 9.2. Yüksek Erişilebilirlik (HA) ve Otomatik Failover
* **Yedek Sunucu (Warm Standby):** İkinci bir yedek VPS üzerinde sistemin pasif bir kopyası hazır tutulur.
* **Cloudflare Otomatik DNS Failover:** Ana Hetzner sunucusu yanıt vermediğinde Cloudflare Health Check mekanizması saniyeler içinde domain A kayıtlarını yedek sunucuya kaydırır.

### 9.3. Vercel Ayrıştırılmış Frontend (Decoupled Dashboard)
* **Bağımsız Panel Arayüzü:** **Panely**'nin frontend arayüzü Vercel üzerinde barındırılır. Hetzner sunucusu tamamen çökse bile yönetim paneli ayakta kalır ve felaket senaryoları yönetilebilir.
* **Statik Bakım Sayfası:** Kesintilerde Cloudflare Edge kuralıyla kullanıcılara anında özelleştirilmiş bakım ekranı sunulur.

---

## 10. Geleceğe Yönelik Mimari Genişleme (Multi-Node / Agent Mimarisi)

* **Merkezi Kontrol Düzlemi (Control Plane):** Panely ana yönetim arayüzü tek bir merkezden çalışır.
* **Ajan (Agent) İletişimi:** İleride eklenecek yeni sunuculara kurulacak hafif sıklet ajanlar, mTLS şifreli kanalı üzerinden ana panelden komut alır. Böylece tek bir **Panely** arayüzünden birden fazla sunucu küme (cluster) halinde yönetilebilir.

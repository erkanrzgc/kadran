# Kadran kalite geliştirme uygulama planı

> Agentic workers: bu plan için subagent-driven-development kullanılır. Sahiplikler çakışmayacak şekilde ayrılır; tamamlanan parçalar önce gereksinim, ardından kod kalitesi açısından incelenir.

**Goal:** 90a6c55 üzerindeki doğrulanmış güvenlik/güvenilirlik kusurlarını kapatmak ve saydam kaynak yönetim masaüstü sunmak.

**Architecture:** Mevcut SSH/gRPC sunucu modeli korunur. Caddy admin yetkisi ayrı, dar bir vekil aracına taşınır. Uygulama yaşam döngüsü ortak koordinatörle seri; masaüstü yönetimi yöntemlere özel JSON-RPC/IPC köprüsüyle çalışır.

**Tech Stack:** Go, protobuf/gRPC, SQLite, systemd, özel Caddy, Electron, React, TypeScript, Vite.

## 1. Başlangıç ve tasarım

- [x] HEAD, çalışma ağacı ve GitHub ana dalını doğrula; 90a6c55'ten yönetilen çalışma alanı aç.
- [x] `docs/specs/2026-10-11-quality-design.md` gereksinimleri ve kabul ölçütlerini kaydet.
- [x] `buf generate` ve `npm ci` çalıştırıldı; ara uygulama testlerindeki kırmızılar giderildi. Nihai Windows/Linux Go ve masaüstü sonuçları tamamlanma kaydında; başlangıçtaki tüm kapıların yeşil olduğu iddia edilmez.

## 2. Güvenli vekil sınırı — security worker

Files: `internal/proxydrv`, yeni `internal/proxyguard`/`cmd/kadran-proxy`, `deploy/systemd`, `internal/bootstrap`, build/package/CI betikleri; güvenlik regresyonları.

- [x] Önce ham static_response/file placeholder, bilinmeyen alan, değiştirilen admin ve unix/loopback upstream isteklerini reddeden davranış testlerini yaz; mevcut açık kırmızı olmalı.
- [x] Daemon erişimini tipli, boyut sınırlı Unix arayüzüne yönlendir; dar girdiden Caddy yapılandırması üret. Caddy ham admin soketi daemon grubuna kapalı olsun.
- [x] systemd kullanıcı/izin/kurulum/yükseltme ve dağıtım paketini birlikte güncelle. Eski kurulumun ham yetkisini bırakma.
- [x] İyi rota geçer ve sakıncalı yapılandırmalar reddedilir; `go test ./internal/proxydrv ./internal/proxyguard ./internal/bootstrap`, ayrı Caddy testleri ve shell kontrolleri doğrulansın.

## 3. Operasyonel koordinasyon — reliability worker

Files: `internal/deploy`, `internal/api`, `internal/health`, `cmd/kadrand`; yeni ortak uygulama koordinatörü ve testleri.

- [x] İnceleme senaryolarını kalıcı regresyonlara çevir: dağıtım sırasında geri alma aktif sürümü durdurmasın; hedef rota atlanınca API başarı dönmesin; yarım HTTP gövdesi hata olsun.
- [x] İptal edilebilir uygulama bazlı ortak sıra kur. API deploy/rollback/update/delete/prune ve gözetmen heal bunu paylaşsın; farklı uygulamalar eşzamanlı çalışabilsin.
- [x] Yanlış upstream dahil proxy sapmasını onar; iyi eski rotayı yükleme başarısızlığında koru; hata semantiği doğru kalsın.
- [x] `go test -race -count=1 ./internal/deploy ./internal/api ./internal/health ./cmd/kadrand` çalıştır. Kuyruk iptali ve üst üste çağrı testleri sınansın.

## 4. Dar yönetim köprüsü — root

Files: `cmd/kadran/sidecar.go`, yeni `sidecar_management.go`, desktop shared/types/bridge/channels/main/preload; protokol, IPC ve sidecar testleri.

- [x] apps.list/get/create/update/delete, app.deploy/rollback/logs, alarms.list, backups.list/create için gerçek protokol ve hataları sınayan testleri yaz.
- [x] Çağrıları mevcut gRPC API'sine bağla; deploy yalnız Succeeded olayıyla başarılıdır. Branch çözümü CLI istemci tarafında mevcut çözümleyiciyi kullanır. Logs sınırlı snapshot; komut çalıştırma yok.
- [x] Yönteme özel IPC alanlarını doğrula; destructive eylemler onay adını taşır; uzun işlemlere ayrı süre, loglara çıktı sınırı uygula.
- [x] UTF-8 akış decoder ve yanlış JSON şekli regresyonlarını ekle/düzelt; başarısız audit doğrulamasının eski yeşil sonucu saklamasını düzelt.
- [x] `go test ./cmd/kadran`, `npm run typecheck`, `npm test` doğrula.

## 5. Cam kaynak yönetimi — frontend worker

Files: desktop renderer ekran/bileşen/stil/i18n, anlamlı davranış testleri ve Electron smoke journey.

- [x] Köprü sözleşmesine göre uygulamalar, uygulama ayrıntısı/sürümler, create/update formu, deploy/rollback onayı, log görünümü, alarmlar ve yedekler ekle.
- [x] Saydam cam stilini, kaynak odaklı navigasyonu, okunabilir operasyon durumlarını ve bağlantı yardımını uygula. Audit ayrıntısı ve doğrulama zamanı görünür olsun.
- [x] 760x520 dahil responsive yerleşim; Türkçe/İngilizce tüm görünür metin, klavye odakları, azaltılmış hareket korunsun.
- [x] Sentetik Electron yönetim yolculuğu hata/başarı/geçiş davranışlarını ve küçük pencere taşmasını ölçsün; ekran görüntülerini incele.

## 6. Özel depolar ve tam sistem doğrulaması

- [x] Özel Git desteğinin mevcut root/daemon ayrımına uygun kimlik bilgisi yolunu tasarla; unsafe Docker URL token veya renderer token ekleme.
- [x] Destek uygulanırsa yalnız sentetik kimlik bilgisiyle redaksiyon, erişim kapsamı ve hata testleri; canlı kanıt sınırını açık tut.
- [x] Yeni parçaları bağımsız gereksinim/kod incelemesinden geçir ve sorunları düzelt.
- [ ] Tam Go yarış testi, go vet, buf lint, güvenlik yüzeyi, masaüstü test/build/Electron ve yeni proxy modülü kontrolleri geçti. Nihai Windows paket açılışı son teslim kapısıdır.

## 7. Dokümantasyon ve teslim

- [x] README/SECURITY/CHANGELOG/karar kaydı güncellenir; ilk bağlantı ve kullanım rehberi, yükseltme, volume/veritabanı tutarlılığı sınırı açık yazılır.
- [x] `docs/quality/2026-10-11-completion.md` dosyasında yapılanlar, testler, gerçek sunucuda doğrulanmayanlar ve başlangıç commit'i yazılır.
- [ ] Sadece doğrulanan değişiklikleri commit et; orijinal dal hâlâ aynı ve temizse fast-forward ile kullanıcının klasörüne taşı. Genel kalite paketi push veya canlı upgrade yapılmaz. Kullanıcının ayrıca istediği logo/README GitHub main değişikliği 138b7cc olarak yayımlandı.

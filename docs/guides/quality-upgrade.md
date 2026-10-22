# Güvenlik ve masaüstü yönetim yükseltmesi

Bu değişiklikler `90a6c55` üstüne hazırlanmıştır. GitHub'daki eski bir release'i indirmek bunları getirmez; güncellenmiş commit'ten üretilen bütün sunucu paketini kullan. Yerel testler canlı sunucuda yükseltme yapıldığı anlamına gelmez.

Son inceleme düzeltmelerini ([K-148](../decisions.md)) içeren commit'i kullan: `e382038` ya da onu birleştiren `main`. Aradaki commit'leri (ör. `fa582e4`) kurma. O commit'lerde iki sorun vardı: sağlık yoklaması büyük HTML ana sayfalı uygulamaları sağlıksız sayıyordu, göç de `kadrand`'ı aracı soketi kurulmadan yeniden başlatıyordu.

## Birlikte yükseltilen parçalar

`kadran`, `kadrand`, `kadran-exec`, `kadran-connect`, `kadran-vault`, `kadran-caddy`, yeni `kadran-proxy` ve systemd/tmpfiles dosyaları aynı paketten gelmelidir. Eski daemon'u yalnız yeni masaüstüyle kullanmak proxy güvenlik sınırını düzeltmez. `scripts/build-release.sh` ve `scripts/package-release.sh` yeni broker'ı pakete ekler. Derleme için Go 1.26.10 veya daha yeni yamalı araç zinciri gerekir; özel Caddy modülü 2.11.7 kullanır.

Bootstrap yükseltmesi eski daemon/Caddy bağlantılarını kapatır, ham admin izinlerini değiştirir ve broker'ı etkinleştirir. Güvenlik geçişi kısa proxy kesintisi gerektirebilir; bakım penceresi ayır. Eski açık admin bağlantısını yalnız chmod ile daraltmak yeterli değildir.

| Yol | İzin / erişim |
|---|---|
| `/run/kadran-proxy/proxy.sock` | Daemon tipli route API'sine erişir |
| `/run/kadran-caddy/admin.sock` | Ham admin yalnız `kadran-proxy` grubuna açıktır |
| `/run/kadran-exec/exec.sock` | Broker ve Caddy erişemez; mevcut executor sınırı korunur |

Sunucuya `kadran-proxy.socket` ve `kadran-proxy.service` kurulmuş olmalı. Caddy veya broker kullanıcısını `kadran`, `docker` gruplarına ekleme. Daemon'u `kadran-proxy` grubuna ekleme. Proxy upstream'leri Docker IPv4 havuzu `172.16.0.0/12` içindedir; farklı havuz kullanan sunucuyu yükseltmeden önce uyumlu hale getir.

## Yükseltmeden önce ve sonra

Önce mevcut sürümü, etkin uygulamaları, rota ve alarm durumunu kaydet; veritabanı, hacim verisi ve vault anahtarının uygun yedeğini doğrula. Bootstrap için README'deki yönetici hedefi ve tam paket yolunu kullan.

Yükseltmeden önce ayrıca şunları ölç:

- **Alan adları.** Her uygulama için `kadran app show <uygulama> <hedef>` çıktısındaki `Domain` değerine bak. Vekil artık API ile aynı kuralı uygular: yalnız küçük harf, en az bir nokta, etiket başına 1–63 karakter (`a-z`, `0-9`, `-`; tire etiketin başında ya da sonunda olamaz), IP adresi değil. Kurala uymayan eski bir kayıt, yükseltmeden sonraki ilk uzlaştırmada canlı rotasını kaybeder. Önce `kadran app update -domain <ad> <uygulama> <hedef>` ile düzelt.
- **Docker adres havuzu.** `sudo docker network inspect $(sudo docker network ls -q) --format '{{.Name}} {{range .IPAM.Config}}{{.Subnet}} {{end}}'` ile her ağın alt ağına bak. Uygulama ağlarının hepsi `172.16.0.0/12` içinde olmalı. Aracı her /16 bloğun ağ adresini, ağ geçidini (`x.y.0.1`, host'un kendisi) ve yayın adresini upstream olarak reddeder. Docker yaklaşık 15 ağdan sonra varsayılan olarak 192.168 havuzuna geçer; o ağdaki uygulamanın rotası reddedilir.
- **Özel Git drop-in'i.** [Özel GitHub depoları](private-git.md) rehberindeki drop-in'i kullanıyorsan yeni `IPAddressDeny=` satırını ekle. Eklemeden ÖNCE DNS'i aynı kısıtla ölç: `sudo systemd-run --wait --pipe -p IPAddressDeny='169.254.0.0/16 fe80::/10' getent hosts api.github.com`. GCP'de `/etc/resolv.conf` çözümleyici olarak `169.254.169.254`'ü gösterebilir; o durumda satır DNS'i de keser.

Sonra `systemctl is-active kadrand kadran-exec kadran-caddy kadran-proxy`, masaüstü bağlantısı, zincir doğrulaması, HTTPS rota, loglar ve yedek listesini kontrol et. `journalctl -u kadrand` içinde `invalid domain` ya da atlanan uygulama satırı olmamalı. Sıradan HTTP yanıtı tek başına dağıtım/rollback kanıtı değildir; canlı sürüm kimliği ve önceki sürümün geri alınabilmesi ayrıca doğrulanmalı.

Geri dönüşte eski bir systemd/socket dosyasını yeniden kurmak ham Caddy yetkisini tekrar açabilir. Güvenlik sınırını koruyan önceki paket veya düzeltme kullan; sürüm geri alma ile uygulama rollback'ini karıştırma. Değişikliklerin yerel kanıtları [tamamlanma kaydında](../quality/2026-10-11-completion.md) bulunur.

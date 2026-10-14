# Günlük operasyon ve kurtarma

## İşlem sırası ve hata anlamı

Aynı uygulamanın dağıtım, geri alma, güncelleme, silme, budama ve otomatik iyileştirmesi ortak bir sırayı paylaşır. Dağıtımın derlemesi, hazır olma kontrolü, trafik değişimi ve eski sürümü boşaltması bitmeden sıradaki değişim başlamaz. Kuyrukta iptal edilen işlem çalışmaz. Farklı uygulamalar bu sırayı paylaşmaz.

Sağlık yoklamasında yalnız HTTP 200 başlığı yeterli değildir; yanıt gövdesinin okunabilmesi gerekir: en fazla 4 KiB okunur, sınıra hatasız ulaşan (daha büyük) gövde sağlıklıdır; okuma hatası, takılma veya sınırın altında kesilme başarısızdır. Kendi proxy rotası eksik olan dağıtım veya geri alma başarısızdır. Vekil sapması alan adı kadar upstream ve yabancı rota açısından da karşılaştırılır ve onarılır. Atlanan aynı domain için doğrulanmış eski rota korunur; bu, farklı bir eski domain'in kalıcı sahiplik kaydı değildir.

Bir istemci zaman aşımı, sunucuda hiçbir değişim olmadığını kanıtlamaz. `app show`, `status`, `alarms`, uygulama logları ve denetim kayıtlarını kontrol et; otomatik tekrar ile yeni bir dağıtım başlatma. Alan adı güncellemesi kaydedilip trafik değişimi başarısız olmuş olabilir; hata ayrıntısı bu durumu belirtir.

## Yedek türleri

| Veri | Koruma | Geri yükleme sınırı |
|---|---|---|
| Kontrol SQLite veritabanı | Saatlik yedek / `kadran backup create` | Uygulama hacimlerini ve executor anahtarını içermez |
| Uygulama hacimleri | İsteğe bağlı [şifreli hacim yedeği](../../deploy/offsite/README.md) | Canlı dosya arşivi; veritabanı snapshot'ı değildir |
| Ortam değerlerini açan anahtar | `/var/lib/kadran-exec/vault.key`, root 0600 | Ayrı ve güvenli kopya gerekir; olmadan mühürlü değerler açılamaz |
| Offsite yedek | [age ile dış kopya](../../deploy/offsite/README.md) | Anahtar yönetimi ve düzenli kurtarma denemesi gerekir |

Veritabanı dosyaları uygulama çalışırken değişebilir. `pg_dump` veya SQLite `.backup` gibi uygulamanın tutarlı çıktısını hacme yaz; ardından arşivle. Uygulama hacmini tek replika yapmak blue-green eski/yeni sürüm çakışmasını kaldırmaz.

## Arıza inceleme

Önce masaüstü Overview/Alarms ve `kadran status`, `kadran alarms`, `kadran app show`, `kadran audit verify` bilgilerini al. Sunucuda `journalctl -u kadrand -u kadran-exec -u kadran-proxy -u kadran-caddy` ilgili bileşenin hatasını ayırır. Zincir “UNVERIFIABLE” ise erişim eksiktir; “BROKEN” ise bütünlük kanıtı bozulmuştur.

Proxy onarımında `kadran-proxy.socket/service` de gerekir. Ham Caddy soketini daemon grubuna açmak bir onarım yöntemi değildir. Docker ağ havuzunun `172.16.0.0/12` sınırına uygun olduğunu kontrol et. Broker dışarı dial edemez; Caddy uygulama trafiği ve ACME bağlantılarını kurar.

Kontrol veritabanını geri yüklemeden önce daemon'u durdur, mevcut dosyanın ve gerekli anahtarların güvenli kopyasını al; sürüme uygun README/backup kurtarma adımlarını uygula. Docker'daki durum ile veritabanındaki geçmişi birlikte doğrula. Bu yükseltmede canlı kurtarma denemesi yapılmış sayılmaz.

## Docker adres havuzu

Uygulama ağları `172.16.0.0/12` içinde olmalı: kadrand sağlık yoklamasını yalnız oraya yapabiliyor, vekil aracısı yalnız oradan upstream kabul ediyor. Docker'ın varsayılan havuzu yaklaşık 15 ağdan sonra `192.168.0.0/16`'ya geçer. Executor ağı oluşturduktan sonra alt ağını okur; havuz dışındaysa kendi oluşturduğu ağı siler ve dağıtım `got subnet … outside 172.16.0.0/12` hatasıyla durur. Önceden var olan ağa dokunmaz (K-150).

`kadran bootstrap`, `/etc/docker/daemon.json` yoksa havuzu sabitleyen dosyayı yazar:

```json
{
  "default-address-pools": [
    {"base": "172.16.0.0/12", "size": 16}
  ]
}
```

dockerd bu ayarı yalnız yeniden başlarken okur. Sunucuda hiç konteyner yoksa bootstrap dockerd'yi hemen yeniden başlatır. Varsa dokunmaz ve kurulumun sonunda uyarır: yeniden başlatma çalışan konteynerleri kısa bir süre durdurur. Bakım penceresinde:

```bash
sudo systemctl restart docker
```

`daemon.json` zaten varsa bootstrap onu değiştirmez. İçinde `default-address-pools` yoksa yukarıdaki anahtarı kendin ekle ve dockerd'yi yeniden başlat. Ayar yalnız yeni ağlara uygulanır; var olan ağların alt ağı değişmez. Ağ başına `/24` veren bir havuz da çalışır, ama aracının ağ geçidi kuralı `/16` varsayıyor ([SECURITY.md](../../SECURITY.md)).

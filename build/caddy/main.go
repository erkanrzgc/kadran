// Command panely-caddy, Panely'nin ters vekilidir: Caddy'nin YALNIZCA
// ihtiyaç duyulan modülleriyle derlenmiş hâli.
//
// ── NEDEN STOK CADDY DEĞİL ──────────────────────────────────────────
//
// panelyd, Caddy'nin admin soketine doğrudan yazıyor (docs/decisions.md
// K-050). Bu yetkinin ne anlama geldiği ÖLÇÜLDÜ, varsayılmadı:
//
//	POST /load ile şu yapılandırma yüklendi:
//	  file_server{ root: /var/lib/caddy/.local/share/caddy, browse: {} }
//	ve Caddy'nin veri dizinindeki dosya düz HTTP üzerinden SERVİS EDİLDİ.
//
// O dizinde alan adlarının TLS ÖZEL ANAHTARLARI durur. Yani stok Caddy
// ile "panelyd ters vekili yapılandırabilir" yetkisi, sessizce "panelyd
// ele geçirilirse alan adının özel anahtarı çalınabilir" demek oluyordu.
//
// Karşı önlem olarak veri dizinini kısıtlamak İŞE YARAMAZ: `file_server`
// dosyayı Caddy'nin KENDİ kimliğiyle okuyor — aynı süreç, aynı uid.
// Süreç içinde sınır yok, systemd sertleştirmesi de yardım etmez çünkü
// Caddy o dizini okuyabilmek zorunda.
//
// Bu yüzden sınır ŞEMA/BINARY düzeyine taşındı: dosya servis eden
// modüller BINARY'DE HİÇ YOK. Doğrulanan değil, TEMSİL EDİLEMEYEN bir
// yetenek — projenin her yerde kullandığı desen (bkz. exec.proto'da
// "host yolu kabul EDİLMEZ, hiç alınmaz").
//
// ── Neden ayrı bir Go modülü? ───────────────────────────────────────
//
// Caddy'nin bağımlılık ağacı büyük. Ana modüle eklemek panely'nin
// go.mod'unu ve `go test ./...` süresini gereksiz yere şişirirdi;
// ayrıca bu binary panely'nin üç binary'siyle aynı sürüm döngüsünde
// DEĞİL.
//
// ⚠ Faz 2 notu: DNS-01 için `caddy-dns/cloudflare` eklentisi buraya
// eklenecek. xcaddy tek başına yetmiyordu zaten — standart modülleri
// ÇIKARMAK için özel bir main.go şart, xcaddy yalnızca ekleyebiliyor.
package main

import (
	caddycmd "github.com/caddyserver/caddy/v2/cmd"

	// ── Çekirdek ────────────────────────────────────────────────────
	//
	// caddyhttp; http uygulamasını, yönlendirme eşleştiricilerini ve
	// `static_response`ı getirir. static_response DOSYA OKUMAZ (gövde
	// yapılandırmada gömülüdür), bu yüzden güvenli ve Faz 5'in bakım
	// sayfası için gerekli.
	_ "github.com/caddyserver/caddy/v2/modules/caddyevents"
	_ "github.com/caddyserver/caddy/v2/modules/caddyevents/eventsconfig"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp"

	// ── Ters vekil: bu binary'nin var olma sebebi ───────────────────
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/headers"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/reverseproxy"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/rewrite"

	// ── Sıkıştırma (dosya sistemine dokunmaz) ───────────────────────
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/encode"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/encode/gzip"
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/encode/zstd"

	// ── TLS / ACME ──────────────────────────────────────────────────
	//
	// filestorage ZORUNLU: sertifikaların diske yazıldığı yer orası.
	// Onsuz Caddy her yeniden başlatmada yeni sertifika ister ve
	// Let's Encrypt oran sınırına çarpar.
	_ "github.com/caddyserver/caddy/v2/modules/caddypki"
	_ "github.com/caddyserver/caddy/v2/modules/caddytls"
	_ "github.com/caddyserver/caddy/v2/modules/caddytls/standardstek"
	_ "github.com/caddyserver/caddy/v2/modules/filestorage"

	// ── Günlükleme ──────────────────────────────────────────────────
	_ "github.com/caddyserver/caddy/v2/modules/caddyhttp/logging"
	_ "github.com/caddyserver/caddy/v2/modules/logging"
)

// ── KASTEN DIŞARIDA BIRAKILANLAR ────────────────────────────────────
//
// Bu liste bir "yapılacaklar" değil, bir GÜVENLİK SINIRIDIR. Buraya bir
// modül eklemek, panelyd'nin yazabildiği yapılandırmanın yapabileceklerini
// genişletir; eklemeden önce yukarıdaki ölçüm tekrarlanmalıdır.
//
//	caddyhttp/fileserver  — file_server + browse. ÖLÇÜLDÜ: TLS özel
//	                        anahtarlarını servis edebiliyor. Bu binary'nin
//	                        var olma sebebi budur.
//	caddyhttp/templates   — `include` ve `readFile` eylemleriyle dosya
//	                        okuyabiliyor; aynı sınıf.
//	caddyfs               — dosya sistemi soyutlaması; yukarıdakileri
//	                        besliyor.
//	caddypki/acmeserver   — Panely bir ACME SUNUCUSU değil, istemcisi.
//	                        Çalıştırmadığımız bir sunucuyu binary'de
//	                        taşımak yalnızca yüzey ekler.
//	reverseproxy/fastcgi  — PHP-FPM köprüsü; Panely konteynere HTTP ile
//	                        konuşur.
//	caddyhttp/caddyauth   — kimlik doğrulama Panely'de SSH katmanında.
//	caddyhttp/push        — HTTP/2 server push, tarayıcılarca terk edildi.
//	metrics, tracing      — Faz 3'ün işi; o zaman ölçülerek eklenir.
func main() {
	caddycmd.Main()
}

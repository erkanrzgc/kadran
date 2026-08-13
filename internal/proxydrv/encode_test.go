package proxydrv

import (
	"encoding/json"
	"strings"
	"testing"
)

// ── Sıkıştırma: vekil AÇTIĞINI yeniden sıkıştırmalı ──────────────────

// TestEveryRouteCompressesBeforeProxying, gerçek sunucuda ölçülen bir
// kusuru sabitler.
//
// Caddy'nin vekil taşıyıcısı upstream'den `Accept-Encoding: gzip` isteyip
// yanıtı ŞEFFAF biçimde açıyor. `encode` işleyicisi yoksa Caddy açtığı
// gövdeyi düz metin olarak gönderiyor — yani upstream sıkıştırma yapsa
// bile istemciye sıkıştırılmamış ulaşıyor.
//
// Ölçüldü (portfolio uygulaması, aynı varlık):
//
//	doğrudan nginx : Content-Encoding: gzip
//	Caddy üzerinden: content-length 311929, Content-Encoding YOK
//	Vercel (CDN)   : 104354 bayt, Content-Encoding: br
//
// Yani 3 kat fazla veri. Bu, tek bir uygulamanın değil, Panely'nin
// servis ettiği HER uygulamanın sorunuydu.
func TestEveryRouteCompressesBeforeProxying(t *testing.T) {
	cfg, err := BuildConfig(BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{
			{AppID: "a", Domain: "a.dev", Upstreams: []Upstream{{Dial: "172.18.0.2:8080"}}},
			{AppID: "b", Domain: "b.dev", Upstreams: []Upstream{{Dial: "172.18.0.3:8080"}}},
		},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}

	for _, r := range cfg.Apps.HTTP.Servers[serverName].Routes {
		if len(r.Handle) < 2 {
			t.Fatalf("%v: işleyici zinciri kısa (%d) — encode eksik olabilir",
				r.Match, len(r.Handle))
		}
		// Sıra TAŞIYICI: encode, reverse_proxy'den ÖNCE gelmeli. Sonra
		// gelseydi hiç çalışmazdı, çünkü vekil yanıtı zaten yazmış olurdu.
		if got := r.Handle[0].Handler; got != "encode" {
			t.Errorf("%v: ilk işleyici %q, beklenen \"encode\"", r.Match, got)
		}
		if got := r.Handle[1].Handler; got != "reverse_proxy" {
			t.Errorf("%v: ikinci işleyici %q, beklenen \"reverse_proxy\"", r.Match, got)
		}
		if len(r.Handle[0].Encodings) == 0 {
			t.Errorf("%v: encodings boş — encode işleyicisi hiçbir şey sıkıştırmaz", r.Match)
		}
	}
}

// TestEncodingsAreLimitedToWhatTheBuildHas, üretilen yapılandırmanın
// özel Caddy derlememizde GERÇEKTEN bulunan kodlayıcılara sınırlı
// kalmasını sağlar.
//
// `panely-caddy list-modules` ölçüldü: http.encoders.gzip ve
// http.encoders.zstd VAR, brotli YOK (eklenti gerektiriyor). Olmayan bir
// kodlayıcı istenirse Caddy yapılandırmanın TAMAMINI reddeder ve o an
// canlı olan bütün rotalar düşer.
func TestEncodingsAreLimitedToWhatTheBuildHas(t *testing.T) {
	cfg, err := BuildConfig(BuildOptions{
		Admin:  testAdmin(),
		Routes: []AppRoute{{AppID: "a", Domain: "a.dev", Upstreams: []Upstream{{Dial: "172.18.0.2:8080"}}}},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}

	enc := cfg.Apps.HTTP.Servers[serverName].Routes[0].Handle[0]
	for name := range enc.Encodings {
		switch name {
		case "gzip", "zstd":
		default:
			t.Errorf("kodlayıcı %q derlemede YOK — Caddy yapılandırmayı tümden reddeder", name)
		}
	}

	// JSON'a doğru biçimde çıkıyor mu? Alan adları Caddy'nin beklediği
	// gibi olmazsa yapılandırma sessizce etkisiz kalır.
	b, err := json.Marshal(enc)
	if err != nil {
		t.Fatalf("serileştirilemedi: %v", err)
	}
	for _, want := range []string{`"handler":"encode"`, `"encodings"`, `"prefer"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("JSON'da %s yok: %s", want, b)
		}
	}
}

package proxydrv

import "testing"

// ── Otomatik HTTPS: sunucu düz HTTP portunu DİNLEMEMELİ ──────────────

// TestServerDoesNotListenOnThePlainHTTPPort, gerçek sunucuda ölçülen bir
// kusuru sabitler.
//
// Tek bir Caddy sunucusu hem :80 hem :443 dinlediğinde, Caddy o sunucu
// için otomatik HTTP→HTTPS yönlendirmesi EKLEMEZ: aynı sunucu iki portu
// da servis ettiği için rotalar düz HTTP üzerinde de yanıt verir.
//
// Ölçüldü: https://panely.erkanrzgc.dev geçerli bir Let's Encrypt
// sertifikasıyla 200 dönerken, http:// AYNI içeriği ŞİFRESİZ 200 ile
// veriyordu — 308 beklenirdi.
//
// Yalnızca HTTPS portu dinlenirse Caddy kendi :80 sunucusunu kurar; o
// sunucu hem ACME HTTP-01 doğrulamasını karşılar hem de yönlendirmeyi
// yapar. Yani düz portu bırakmak bir yetenek kaybı DEĞİL.
func TestServerDoesNotListenOnThePlainHTTPPort(t *testing.T) {
	cfg, err := BuildConfig(BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{{
			AppID:     "a",
			Domain:    "ornek.dev",
			Upstreams: []Upstream{{Dial: "172.18.0.2:8080"}},
		}},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}

	srv := cfg.Apps.HTTP.Servers[serverName]
	for _, l := range srv.Listen {
		if l == ":80" {
			t.Errorf("sunucu :80 dinliyor — otomatik HTTP→HTTPS yönlendirmesi "+
				"bastırılır ve site ŞİFRESİZ servis edilir (listen=%v)", srv.Listen)
		}
	}
	if len(srv.Listen) != 1 || srv.Listen[0] != ":443" {
		t.Errorf("listen = %v, beklenen [:443]", srv.Listen)
	}
}

// TestCustomPortsReachCaddysOwnPortSettings, özel port verildiğinde
// Caddy'nin YÖNLENDİRME hedefini doğru hesaplamasını sağlar.
//
// Caddy "hangi port HTTPS'tir" bilgisini uygulama düzeyindeki
// http_port/https_port alanlarından okur. Sunucu özel bir portu dinleyip
// bu alanlar boş kalsaydı Caddy 443'ü varsayar ve yönlendirmeyi YANLIŞ
// porta yapardı — sessizce, çünkü HTTPS tarafı yine çalışırdı.
func TestCustomPortsReachCaddysOwnPortSettings(t *testing.T) {
	cfg, err := BuildConfig(BuildOptions{
		Admin:     testAdmin(),
		HTTPPort:  8080,
		HTTPSPort: 8443,
		Routes: []AppRoute{{
			AppID:     "a",
			Domain:    "ornek.dev",
			Upstreams: []Upstream{{Dial: "172.18.0.2:8080"}},
		}},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}
	if got := cfg.Apps.HTTP.Servers[serverName].Listen; len(got) != 1 || got[0] != ":8443" {
		t.Errorf("listen = %v, beklenen [:8443]", got)
	}
	if got := cfg.Apps.HTTP.HTTPPort; got != 8080 {
		t.Errorf("http_port = %d, beklenen 8080", got)
	}
	if got := cfg.Apps.HTTP.HTTPSPort; got != 8443 {
		t.Errorf("https_port = %d, beklenen 8443", got)
	}
}

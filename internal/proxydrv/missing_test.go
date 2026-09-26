package proxydrv

import (
	"slices"
	"testing"
)

func yapilandirma(t *testing.T, rotalar ...AppRoute) *Config {
	t.Helper()
	cfg, err := BuildConfig(BuildOptions{
		Admin:  Admin{Listen: "fd/3", Origins: []string{"localhost"}},
		Routes: rotalar,
	})
	if err != nil {
		t.Fatalf("yapılandırma kurulamadı: %v", err)
	}
	return cfg
}

func rota(app, host, dial string) AppRoute {
	return AppRoute{AppID: app, Domain: host, Upstreams: []Upstream{{Dial: dial}}}
}

// TestMissingHosts, izleyicinin TETİKLEYİCİSİNİ sınar (K-055, K-112).
//
// Tetik YALNIZCA tek yönlü: beklenen bir alan adının canlıda HİÇ rotası
// yoksa. Taze sunucu testinde ölçülen arıza tam buydu — Caddy yeniden
// başlayınca rotasız açıldı ve site panelyd yeniden başlayana kadar
// kapalı kaldı.
//
// Fazla rotalar ve upstream farkları BİLEREK tetik değil: uzlaştırma
// sağlıksız bir uygulamayı ATLIYOR ve canlıdaki eski rotası iyileştirme
// bitene kadar duruyor. İki yönlü bir tetik o rotayı silip gözetmenin
// davranışını değiştirirdi.
func TestMissingHosts(t *testing.T) {
	ikisi := yapilandirma(t,
		rota("blog", "blog.example.com", "172.18.0.5:8080"),
		rota("shop", "shop.example.com", "172.19.0.4:3000"))

	durumlar := []struct {
		ad    string
		want  *Config
		live  *Config
		eksik []string
	}{
		{"Caddy rotasız açıldı (yeniden başlatma)", ikisi, &Config{}, []string{"blog.example.com", "shop.example.com"}},
		{"canlı hiç okunamadı (nil)", ikisi, nil, []string{"blog.example.com", "shop.example.com"}},
		{"her şey yerinde", ikisi, ikisi, nil},
		{"biri eksik", ikisi, yapilandirma(t, rota("blog", "blog.example.com", "172.18.0.5:8080")), []string{"shop.example.com"}},
		{"KONTROL: fazla rota tetik DEĞİL", yapilandirma(t, rota("blog", "blog.example.com", "172.18.0.5:8080")), ikisi, nil},
		{"KONTROL: upstream farkı tetik DEĞİL", ikisi,
			yapilandirma(t, rota("blog", "blog.example.com", "172.18.0.9:8080"), rota("shop", "shop.example.com", "172.19.0.4:3000")), nil},
		{"beklenen boş", yapilandirma(t), &Config{}, nil},
	}
	for _, d := range durumlar {
		t.Run(d.ad, func(t *testing.T) {
			got := MissingHosts(d.want, d.live)
			if !slices.Equal(got, d.eksik) {
				t.Errorf("MissingHosts = %v, beklenen %v", got, d.eksik)
			}
		})
	}
}

// TestLiveUpstreams, onarım sırasında ATLANAN bir uygulamanın canlı
// upstream'lerinin taşınabilmesini — ve yalnızca GEÇERLİ olanların
// taşınmasını — sınar.
//
// Canlı yapılandırma güvenilir kaynak değil: admin soketine panelyd'den
// başka biri de yazabilir (K-054). Taşınan her adres NewUpstream'den
// yeniden geçiyor; soket yolu, ad ya da belirsiz adres geçemez.
func TestLiveUpstreams(t *testing.T) {
	canli := &Config{Apps: &Apps{HTTP: &HTTPApp{Servers: map[string]*HTTPServer{"s": {
		Routes: []Route{
			{Match: []Match{{Host: []string{"shop.example.com"}}},
				Handle: []Handler{{Handler: "reverse_proxy", Upstreams: []Upstream{{Dial: "172.19.0.4:3000"}}}}},
			{Match: []Match{{Host: []string{"kotu.example.com"}}},
				Handle: []Handler{{Handler: "reverse_proxy", Upstreams: []Upstream{
					{Dial: "unix//run/docker.sock"}, {Dial: "localhost:80"}, {Dial: "0.0.0.0:80"}, {Dial: "172.19.0.9:0"},
				}}}},
		},
	}}}}}

	if got := LiveUpstreams(canli, "shop.example.com"); !slices.Equal(got, []Upstream{{Dial: "172.19.0.4:3000"}}) {
		t.Errorf("geçerli upstream taşınmadı: %v", got)
	}
	if got := LiveUpstreams(canli, "kotu.example.com"); len(got) != 0 {
		t.Errorf("geçersiz upstream taşındı: %v", got)
	}
	if got := LiveUpstreams(canli, "yok.example.com"); got != nil {
		t.Errorf("olmayan alan adı için upstream döndü: %v", got)
	}
	if got := LiveUpstreams(nil, "shop.example.com"); got != nil {
		t.Errorf("nil yapılandırmada upstream döndü: %v", got)
	}
}

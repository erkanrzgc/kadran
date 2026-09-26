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

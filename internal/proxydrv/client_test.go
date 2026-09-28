package proxydrv

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// Admin istemcisi yalnızca gerçek sunucuda sınanmıştı (K-054): paket
// testleri `Load`'un "gönder, geri oku, karşılaştır" akışını, Caddy'nin
// hata mesajının iletilmesini ve boş yanıtı hiç çalıştırmıyordu.

// sahteAdmin, Caddy admin API'sinin bu istemcinin kullandığı iki ucunu
// taklit eder. `canli` nil değilse GET /config/ onu döndürür (başkası
// yazmış gibi); değilse son POST /load gövdesini.
type sahteAdmin struct {
	mu         sync.Mutex
	yuklenen   []byte
	canli      []byte
	loadKodu   int
	loadGovde  string
	hostlar    []string
	yollar     []string
	icerikTuru string
}

func (a *sahteAdmin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.hostlar = append(a.hostlar, r.Host)
	a.yollar = append(a.yollar, r.Method+" "+r.URL.Path)
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/load":
		a.icerikTuru = r.Header.Get("Content-Type")
		if a.loadKodu != 0 {
			w.WriteHeader(a.loadKodu)
			_, _ = io.WriteString(w, a.loadGovde)
			return
		}
		a.yuklenen, _ = io.ReadAll(r.Body)
	case r.Method == http.MethodGet && r.URL.Path == "/config/":
		if a.canli != nil {
			_, _ = w.Write(a.canli)
			return
		}
		_, _ = w.Write(a.yuklenen)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func adminKur(t *testing.T, a *sahteAdmin) *Client {
	t.Helper()
	// Admin istemcisi yalnızca Linux'ta koşuyor (panelyd). Windows'ta
	// unix soketine Listen başarılı ama Dial "An invalid argument was
	// supplied" veriyor — ölçüldü, kısa ad, uzun ad ve os.TempDir ile üç
	// ayrı yolda; yol uzunluğu DEĞİL. Bu testler CI'ın Linux işlerinde
	// koşuyor; orada atlanmaları YASAK (bkz. aşağıdaki Fatal).
	if runtime.GOOS == "windows" {
		t.Skip("unix soketine bağlanma bu platformda çalışmıyor (ölçüldü)")
	}
	sock := filepath.Join(t.TempDir(), "admin.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("unix soketi açılamadı: %v", err)
	}
	srv := &http.Server{Handler: a}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return New(sock)
}

func ikiRotaliYapilandirma(t *testing.T, ekstra ...AppRoute) *Config {
	t.Helper()
	rotalar := append([]AppRoute{
		{AppID: "blog", Domain: "blog.example.com", Upstreams: []Upstream{{Dial: "172.18.0.5:8080"}}},
		{AppID: "shop", Domain: "shop.example.com", Upstreams: []Upstream{{Dial: "172.19.0.4:3000"}}},
	}, ekstra...)
	cfg, err := BuildConfig(BuildOptions{Admin: Admin{Listen: "unix//x.sock", Origins: []string{adminHost}}, Routes: rotalar})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestClientLoadPostsAndReadsBack(t *testing.T) {
	a := &sahteAdmin{}
	c := adminKur(t, a)

	if err := c.Load(context.Background(), ikiRotaliYapilandirma(t)); err != nil {
		t.Fatalf("yükleme başarısız: %v", err)
	}
	if strings.Join(a.yollar, ",") != "POST /load,GET /config/" {
		t.Errorf("istek sırası %v — önce yükleme, sonra GERİ OKUMA bekleniyordu", a.yollar)
	}
	for _, h := range a.hostlar {
		if h != adminHost {
			t.Errorf("Host %q — Caddy origins listesinde olmayan Host'u 403 ile reddediyor", h)
		}
	}
	if a.icerikTuru != "application/json" {
		t.Errorf("Content-Type %q", a.icerikTuru)
	}
	var geri Config
	if err := json.Unmarshal(a.yuklenen, &geri); err != nil {
		t.Fatalf("gönderilen gövde yapılandırma değil: %v", err)
	}
}

// TestClientLoadRejectsARouteItDidNotSend: 200 aldık diye canlının bizim
// yapılandırmamız olduğu varsayılamaz — admin soketine başkası da
// yazabilir (K-054).
func TestClientLoadRejectsARouteItDidNotSend(t *testing.T) {
	yabanci, err := json.Marshal(ikiRotaliYapilandirma(t,
		AppRoute{AppID: "yabanci", Domain: "evil.example.com", Upstreams: []Upstream{{Dial: "10.0.0.9:80"}}}))
	if err != nil {
		t.Fatal(err)
	}
	c := adminKur(t, &sahteAdmin{canli: yabanci})

	err = c.Load(context.Background(), ikiRotaliYapilandirma(t))
	if err == nil || !strings.Contains(err.Error(), "evil.example.com") {
		t.Fatalf("gönderilmeyen rota yakalanmadı: %v", err)
	}
}

func TestClientLoadRejectsADifferentUpstream(t *testing.T) {
	farkli := ikiRotaliYapilandirma(t)
	degisti := 0
	for _, srv := range farkli.Apps.HTTP.Servers {
		for i := range srv.Routes {
			for j := range srv.Routes[i].Handle {
				for k := range srv.Routes[i].Handle[j].Upstreams {
					if srv.Routes[i].Handle[j].Upstreams[k].Dial == "172.18.0.5:8080" {
						srv.Routes[i].Handle[j].Upstreams[k].Dial = "172.18.0.99:8080"
						degisti++
					}
				}
			}
		}
	}
	if degisti != 1 {
		t.Fatalf("kurulum yanlış: %d upstream değişti", degisti)
	}
	govde, err := json.Marshal(farkli)
	if err != nil {
		t.Fatal(err)
	}
	c := adminKur(t, &sahteAdmin{canli: govde})

	// Hatanın KENDİSİ doğrulanıyor: bağlantı hatası da "hata" olurdu ve
	// bu test ilk hâlinde tam olarak böyle, yanlış sebeple geçiyordu.
	err = c.Load(context.Background(), ikiRotaliYapilandirma(t))
	if err == nil || !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("upstream farkı yakalanmadı: %v", err)
	}
}

// TestClientLoadReportsCaddysMessage: Caddy yapılandırmayı reddederse
// sebep kullanıcıya ulaşmalı — ham JSON değil, mesajın kendisi.
func TestClientLoadReportsCaddysMessage(t *testing.T) {
	c := adminKur(t, &sahteAdmin{loadKodu: http.StatusBadRequest,
		loadGovde: `{"error":"loading config: unknown module"}`})

	err := c.Load(context.Background(), ikiRotaliYapilandirma(t))
	if err == nil {
		t.Fatal("400 yanıtı başarı sayıldı")
	}
	if !strings.Contains(err.Error(), "unknown module") || strings.Contains(err.Error(), `{"error"`) {
		t.Errorf("Caddy'nin mesajı ayıklanmadı: %v", err)
	}
}

// TestClientCurrentTreatsEmptyAsNoConfig: yapılandırmasız Caddy boş ya da
// "null" döner; bu bir hata değil, boş yapılandırma.
func TestClientCurrentTreatsEmptyAsNoConfig(t *testing.T) {
	for _, govde := range []string{"", "null", "  null\n"} {
		c := adminKur(t, &sahteAdmin{canli: []byte(govde)})
		cfg, err := c.Current(context.Background())
		if err != nil {
			t.Errorf("%q: hata %v", govde, err)
			continue
		}
		if cfg == nil || len(routesByHost(cfg)) != 0 {
			t.Errorf("%q: boş yapılandırma beklenirken %+v", govde, cfg)
		}
	}
}

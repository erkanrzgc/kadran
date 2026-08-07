package proxydrv

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ── NewUpstream: `dial` KURULUYOR, alınmıyor ─────────────────────────

// TestUpstreamRejectsAnythingButAnIP, K-050'nin yükümlülüğünü doğrular.
//
// Serbest bir `dial` dizesi, ele geçirilmiş bir panelyd'nin ayrıcalıklı
// executor soketini internete açmasına izin verirdi. Adres bir IP olarak
// AYRIŞTIRILIYOR, yani bu girdilerin hiçbiri temsil edilemiyor.
func TestUpstreamRejectsAnythingButAnIP(t *testing.T) {
	cases := []string{
		"unix//run/panely-exec/exec.sock",
		"unix//run/panely/api.sock",
		"localhost",
		"panely_hello_r1_0",
		"127.0.0.1:8080",         // port ayrı alanda verilmeli
		"http://127.0.0.1",       // şema
		"0.0.0.0",                // belirsiz
		"",                       // boş
		"127.0.0.1 ",             // boşluk
		"127.0.0.1\nX-Injected:", // satır sonu
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			if u, err := NewUpstream(in, 8080); err == nil {
				t.Fatalf("kabul edildi: %q → %q", in, u.Dial)
			}
		})
	}
}

func TestUpstreamBuildsDialFromParts(t *testing.T) {
	u, err := NewUpstream("172.18.0.3", 8080)
	if err != nil {
		t.Fatalf("kurulamadı: %v", err)
	}
	if u.Dial != "172.18.0.3:8080" {
		t.Errorf("dial = %q", u.Dial)
	}

	// IPv6 köşeli parantezle biçimlenmeli, yoksa port ayrıştırılamaz.
	u6, err := NewUpstream("fd00::2", 8080)
	if err != nil {
		t.Fatalf("IPv6 kurulamadı: %v", err)
	}
	if u6.Dial != "[fd00::2]:8080" {
		t.Errorf("IPv6 dial = %q", u6.Dial)
	}
}

func TestUpstreamRejectsBadPort(t *testing.T) {
	for _, p := range []uint32{0, 65536, 1 << 20} {
		if _, err := NewUpstream("172.18.0.3", p); err == nil {
			t.Errorf("port %d kabul edildi", p)
		}
	}
}

// ── BuildConfig ──────────────────────────────────────────────────────

func testAdmin() Admin {
	return Admin{Listen: "fd/3", Origins: []string{"localhost"}}
}

func mustUpstream(t *testing.T, ip string, port uint32) Upstream {
	t.Helper()
	u, err := NewUpstream(ip, port)
	if err != nil {
		t.Fatalf("upstream: %v", err)
	}
	return u
}

// TestBuildConfigAlwaysCarriesAdmin, kilitlenmeyi önleyen kuralı
// doğrular.
//
// `POST /load` kök nesnenin TAMAMINI değiştiriyor. Admin bloğu olmayan
// bir yapılandırma yüklenirse Caddy varsayılana (TCP :2019) döner ve
// panelyd unix soketinden bir daha ULAŞAMAZ — sistem kendini kilitler.
func TestBuildConfigAlwaysCarriesAdmin(t *testing.T) {
	if _, err := BuildConfig(BuildOptions{}); err == nil {
		t.Fatal("admin bloğu olmadan yapılandırma üretildi — panelyd kilitlenirdi")
	}

	cfg, err := BuildConfig(BuildOptions{Admin: testAdmin()})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}
	if cfg.Admin.Listen != "fd/3" {
		t.Errorf("admin.listen = %q", cfg.Admin.Listen)
	}
	// origins boş kalırsa Caddy her isteği 403 ile reddeder (ölçüldü).
	if len(cfg.Admin.Origins) == 0 {
		t.Error("admin.origins boş — her POST /load 403 alırdı")
	}
}

func TestBuildConfigRoutesByHost(t *testing.T) {
	cfg, err := BuildConfig(BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{
			{AppID: "blog", Domain: "blog.example.com",
				Upstreams: []Upstream{mustUpstream(t, "172.18.0.3", 8080)}},
			{AppID: "shop", Domain: "shop.example.com",
				Upstreams: []Upstream{mustUpstream(t, "172.19.0.2", 3000)}},
		},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}

	got := routesByHost(cfg)
	if len(got["blog.example.com"]) != 1 || got["blog.example.com"][0] != "172.18.0.3:8080" {
		t.Errorf("blog rotası: %v", got["blog.example.com"])
	}
	if len(got["shop.example.com"]) != 1 || got["shop.example.com"][0] != "172.19.0.2:3000" {
		t.Errorf("shop rotası: %v", got["shop.example.com"])
	}
}

// TestBuildConfigIsDeterministic, aynı girdiden aynı JSON çıktığını
// doğrular.
//
// Belirlenimli olmasaydı "yüklediğim şey canlıda mı" karşılaştırması her
// seferinde gürültü üretir ve kontrol kapatılmaya mahkûm olurdu.
func TestBuildConfigIsDeterministic(t *testing.T) {
	opts := BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{
			{AppID: "shop", Domain: "s.example.com", Upstreams: []Upstream{mustUpstream(t, "10.0.0.2", 80)}},
			{AppID: "blog", Domain: "b.example.com", Upstreams: []Upstream{mustUpstream(t, "10.0.0.3", 80)}},
			{AppID: "api", Domain: "a.example.com", Upstreams: []Upstream{mustUpstream(t, "10.0.0.4", 80)}},
		},
	}

	var first string
	for range 5 {
		cfg, err := BuildConfig(opts)
		if err != nil {
			t.Fatalf("üretilemedi: %v", err)
		}
		b, err := json.Marshal(cfg)
		if err != nil {
			t.Fatalf("kodlanamadı: %v", err)
		}
		if first == "" {
			first = string(b)
			continue
		}
		if string(b) != first {
			t.Fatal("aynı girdiden farklı JSON üretildi")
		}
	}
}

// TestBuildConfigRejectsDuplicateDomain, iki uygulamanın aynı alan adına
// atanmasını reddeder: hangisinin kazandığı rota sırasına bağlı olurdu.
func TestBuildConfigRejectsDuplicateDomain(t *testing.T) {
	_, err := BuildConfig(BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{
			{AppID: "a", Domain: "x.example.com", Upstreams: []Upstream{mustUpstream(t, "10.0.0.2", 80)}},
			{AppID: "b", Domain: "x.example.com", Upstreams: []Upstream{mustUpstream(t, "10.0.0.3", 80)}},
		},
	})
	if err == nil {
		t.Fatal("aynı alan adı iki uygulamaya atanabildi")
	}
	if !strings.Contains(err.Error(), "x.example.com") {
		t.Errorf("hata hangi alan adı olduğunu söylemiyor: %v", err)
	}
}

// TestBuildConfigRejectsRouteWithoutUpstream, trafiği hiçbir yere
// göndermeyen bir rotanın üretilemeyeceğini doğrular — uygulamayı
// sessizce 502 yapardı.
func TestBuildConfigRejectsRouteWithoutUpstream(t *testing.T) {
	_, err := BuildConfig(BuildOptions{
		Admin:  testAdmin(),
		Routes: []AppRoute{{AppID: "blog", Domain: "b.example.com"}},
	})
	if err == nil {
		t.Fatal("upstream'siz rota üretildi")
	}
}

// TestAppWithoutDomainIsNotProxied, alan adı olmayan uygulamanın vekilde
// hiç görünmediğini doğrular. Bu geçerli bir durum: yalnızca iç ağdan
// erişilen uygulamalar var.
func TestAppWithoutDomainIsNotProxied(t *testing.T) {
	cfg, err := BuildConfig(BuildOptions{
		Admin:  testAdmin(),
		Routes: []AppRoute{{AppID: "worker", Upstreams: []Upstream{mustUpstream(t, "10.0.0.2", 80)}}},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}
	if cfg.Apps != nil {
		t.Errorf("alan adı olmayan uygulama vekile girdi: %+v", cfg.Apps)
	}
}

// ── Load: geri okuma ─────────────────────────────────────────────────

// fakeCaddy, admin API'yi taklit eder.
type fakeCaddy struct {
	live      *Config
	loadCode  int
	loadCalls int
	// drift, /load'ın kabul ettiği ama canlıya YANSITMADIĞI durumu
	// simüle eder (başka bir süreç üzerine yazmış gibi).
	drift bool
	seen  []string
}

func (f *fakeCaddy) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen = append(f.seen, r.Method+" "+r.URL.Path+" host="+r.Host)
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			f.loadCalls++
			if f.loadCode != 0 && f.loadCode != http.StatusOK {
				w.WriteHeader(f.loadCode)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "sahte ret"})
				return
			}
			var cfg Config
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if !f.drift {
				f.live = &cfg
			}
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			if f.live == nil {
				_, _ = w.Write([]byte("null"))
				return
			}
			_ = json.NewEncoder(w).Encode(f.live)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// newFakeClient, unix soketi üzerinden sahte Caddy'ye bağlanan GERÇEK
// Client'ı kurar.
//
// Unix soketi kullanılıyor çünkü sınanan şey New()'in kurduğu taşıma da
// dahil — hatanın yaşayabileceği yer orası. Windows'ta AF_UNIX Go
// tarafından destekleniyor ama yol uzunluğu sınırı var; sorun çıkarsa
// test atlanıyor ve bu AÇIKÇA duyuruluyor.
func newFakeClient(t *testing.T, f *fakeCaddy) *Client {
	t.Helper()

	// ⚠ Windows'ta ATLANIYOR ve bu, garanti ortamda atlama YASAĞINI
	// ihlal etmiyor: panelyd yalnızca Linux'ta çalışıyor (systemd birimi,
	// unix soketleri, SO_PEERCRED). Windows burada bir hedef değil,
	// geliştirme makinesi.
	//
	// Windows'ta `net.Listen("unix", …)` BAŞARILI oluyor ama Go'nun HTTP
	// taşıması aynı sokete bağlanamıyor ("An invalid argument was
	// supplied") — yani atlamayı Listen hatasına bağlamak İŞE YARAMIYORDU.
	//
	// CI'ın Linux işleri (ubuntu-latest ve ubuntu-24.04-arm) bu testleri
	// GERÇEKTEN koşturuyor; yerelde de WSL üzerinden doğrulandı.
	if runtime.GOOS == "windows" {
		t.Skip("panelyd Linux'ta çalışır; unix soketi taşıması burada sınanamıyor")
	}

	sock := filepath.Join(t.TempDir(), "admin.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatalf("unix soketi açılamadı: %v", err)
	}
	srv := &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: f.handler()}, //nolint:gosec // test sunucusu
	}
	srv.Start()
	t.Cleanup(func() { srv.Close(); _ = os.Remove(sock) })

	return New(sock)
}

func TestLoadSendsAdminHost(t *testing.T) {
	f := &fakeCaddy{}
	c := newFakeClient(t, f)

	cfg, err := BuildConfig(BuildOptions{Admin: testAdmin()})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}
	if err := c.Load(context.Background(), cfg); err != nil {
		t.Fatalf("yüklenemedi: %v", err)
	}

	// Caddy Host'u doğruluyor; yanlış Host 403 "host not allowed" alır
	// (gerçek sunucuda ölçüldü).
	for _, s := range f.seen {
		if !strings.Contains(s, "host=localhost") {
			t.Errorf("istek yanlış Host ile gitti: %s", s)
		}
	}
}

// TestLoadDetectsConfigThatDidNotApply, "200 aldım" ile "canlıda benim
// yapılandırmam var" arasındaki farkı doğrular.
//
// Sahte Caddy /load'ı KABUL ediyor ama canlıya yansıtmıyor — admin
// soketine yazan başka bir sürecin üzerine yazması gibi. Geri okuma
// olmasaydı panelyd bunu hiç fark etmez ve SQLite canlı olmayan bir
// durumu tarif ederdi.
func TestLoadDetectsConfigThatDidNotApply(t *testing.T) {
	f := &fakeCaddy{drift: true}
	c := newFakeClient(t, f)

	cfg, err := BuildConfig(BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{{AppID: "blog", Domain: "b.example.com",
			Upstreams: []Upstream{mustUpstream(t, "172.18.0.3", 8080)}}},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}

	err = c.Load(context.Background(), cfg)
	if err == nil {
		t.Fatal("uygulanmayan yapılandırma başarılı sayıldı")
	}
	if !strings.Contains(err.Error(), "BULUNAMADI") {
		t.Errorf("hata sebebi açık değil: %v", err)
	}
	if f.loadCalls != 1 {
		t.Errorf("/load %d kez çağrıldı", f.loadCalls)
	}
}

// TestLoadDetectsChangedUpstreams, alan adı doğru ama upstream'i farklı
// olan canlı bir yapılandırmanın yakalandığını doğrular.
//
// Yalnızca "rota var mı" bakılsaydı, başka bir sürecin trafiği başka bir
// arka uca çevirmesi görünmezdi.
func TestLoadDetectsChangedUpstreams(t *testing.T) {
	f := &fakeCaddy{}
	c := newFakeClient(t, f)

	cfg, err := BuildConfig(BuildOptions{
		Admin: testAdmin(),
		Routes: []AppRoute{{AppID: "blog", Domain: "b.example.com",
			Upstreams: []Upstream{mustUpstream(t, "172.18.0.3", 8080)}}},
	})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}
	if err := c.Load(context.Background(), cfg); err != nil {
		t.Fatalf("ilk yükleme: %v", err)
	}

	// Başka biri upstream'i değiştirdi.
	f.live.Apps.HTTP.Servers[serverName].Routes[0].Handle[0].Upstreams =
		[]Upstream{{Dial: "10.66.66.66:8080"}}
	f.drift = true

	if err := c.Load(context.Background(), cfg); err == nil {
		t.Fatal("değişmiş upstream fark edilmedi")
	}
}

func TestLoadReportsCaddyError(t *testing.T) {
	f := &fakeCaddy{loadCode: http.StatusBadRequest}
	c := newFakeClient(t, f)

	cfg, err := BuildConfig(BuildOptions{Admin: testAdmin()})
	if err != nil {
		t.Fatalf("üretilemedi: %v", err)
	}
	err = c.Load(context.Background(), cfg)
	if err == nil {
		t.Fatal("Caddy'nin reddi başarı sayıldı")
	}
	if !strings.Contains(err.Error(), "sahte ret") {
		t.Errorf("Caddy'nin mesajı taşınmadı: %v", err)
	}
}

func TestCurrentHandlesEmptyConfig(t *testing.T) {
	f := &fakeCaddy{}
	c := newFakeClient(t, f)

	cfg, err := c.Current(context.Background())
	if err != nil {
		t.Fatalf("okunamadı: %v", err)
	}
	if cfg == nil {
		t.Fatal("boş yapılandırmada nil döndü")
	}
}

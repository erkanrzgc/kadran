package proxydrv

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func yonSet(redirects ...ProxyRedirect) RouteSet {
	return RouteSet{
		Routes: []ProxyRoute{
			{Domain: "example.com", Upstreams: []ProxyUpstream{{IP: "172.18.0.3", Port: 8080}}},
			{Domain: "other.example", Upstreams: []ProxyUpstream{{IP: "172.18.0.4", Port: 8080}}},
		},
		Redirects: redirects,
	}
}

// yonlendirmeRotasi, FixedConfig çıktısındaki from için üretilen rotayı verir.
func yonlendirmeRotasi(t *testing.T, cfg *Config, from string) Route {
	t.Helper()
	for _, r := range cfg.Apps.HTTP.Servers[serverName].Routes {
		if len(r.Match) == 1 && r.Match[0].Host[0] == from {
			return r
		}
	}
	t.Fatalf("%s için rota yok", from)
	return Route{}
}

func TestBuildConfigEmitsRedirectRoute(t *testing.T) {
	cfg, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(yonlendirmeRotasi(t, cfg, "www.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/redirect-route.json")
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := json.Compact(&want, raw); err != nil {
		t.Fatal(err)
	}
	if string(got) != want.String() {
		t.Fatalf("yönlendirme rotası altın dosyadan farklı:\n got %s\nwant %s", got, want.String())
	}
}

func TestBuildConfigPlacesRedirectsAfterDomainRoutes(t *testing.T) {
	cfg, err := FixedConfig(yonSet(
		ProxyRedirect{From: "www.other.example", To: "other.example"},
		ProxyRedirect{From: "www.example.com", To: "example.com"},
	))
	if err != nil {
		t.Fatal(err)
	}
	var hosts []string
	for _, r := range cfg.Apps.HTTP.Servers[serverName].Routes {
		hosts = append(hosts, r.Match[0].Host[0])
	}
	want := []string{"example.com", "other.example", "www.example.com", "www.other.example"}
	if !reflect.DeepEqual(hosts, want) {
		t.Fatalf("rota sırası %v, beklenen %v", hosts, want)
	}
}

func TestBuildConfigRejectsRedirectNameClashes(t *testing.T) {
	up := []Upstream{{Dial: "172.18.0.3:8080"}}
	cases := map[string][]AppRoute{
		"domaine eşit": {
			{AppID: "a", Domain: "example.com", Upstreams: up, Redirects: []string{"other.example"}},
			{AppID: "b", Domain: "other.example", Upstreams: up},
		},
		"önceki yönlendirmeye eşit": {
			{AppID: "a", Domain: "example.com", Upstreams: up, Redirects: []string{"www.example.com"}},
			{AppID: "b", Domain: "other.example", Upstreams: up, Redirects: []string{"www.example.com"}},
		},
		"geçersiz ad": {
			{AppID: "a", Domain: "example.com", Upstreams: up, Redirects: []string{"A.com"}},
		},
	}
	for name, routes := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := BuildConfig(BuildOptions{Admin: Admin{Listen: "fd/3"}, Routes: routes})
			if err == nil {
				t.Fatal("hata beklenirdi")
			}
		})
	}
}

func TestFixedConfigRejectsRedirectToUnroutedDomain(t *testing.T) {
	_, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "evil.example"}))
	if err == nil || !strings.Contains(err.Error(), "not a routed domain") {
		t.Fatalf("hata beklenirdi: %v", err)
	}
}

func TestFixedConfigRejectsRedirectFromRoutedDomain(t *testing.T) {
	if _, err := FixedConfig(yonSet(ProxyRedirect{From: "other.example", To: "example.com"})); err == nil {
		t.Fatal("yönlendirilen domain kaynak olamaz")
	}
}

func TestFixedConfigRejectsDuplicateRedirect(t *testing.T) {
	_, err := FixedConfig(yonSet(
		ProxyRedirect{From: "www.example.com", To: "example.com"},
		ProxyRedirect{From: "www.example.com", To: "other.example"},
	))
	if err == nil {
		t.Fatal("tekrarlanan kaynak reddedilmeliydi")
	}
}

func TestFixedConfigRejectsTooManyRedirects(t *testing.T) {
	var rs []ProxyRedirect
	for i := 0; i <= MaxRedirects; i++ {
		rs = append(rs, ProxyRedirect{From: fmt.Sprintf("w%d.example.com", i), To: "example.com"})
	}
	if len(rs) != 1025 {
		t.Fatalf("test girdisi %d", len(rs))
	}
	if _, err := FixedConfig(yonSet(rs...)); err == nil {
		t.Fatal("1025 yönlendirme reddedilmeliydi")
	}
	if _, err := FixedConfig(yonSet(rs[:MaxRedirects]...)); err != nil {
		t.Fatalf("1024 yönlendirme kabul edilmeliydi: %v", err)
	}
}

func TestFixedConfigRejectsInvalidRedirectName(t *testing.T) {
	for _, bad := range []string{"A.com", "1.2.3.4", "a{b}.com", "", "a.com/{file./x}"} {
		if _, err := FixedConfig(yonSet(ProxyRedirect{From: bad, To: "example.com"})); err == nil {
			t.Errorf("from %q kabul edildi", bad)
		}
		if _, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: bad})); err == nil {
			t.Errorf("to %q kabul edildi", bad)
		}
	}
}

func TestSafeRoutesRoundTripsRedirects(t *testing.T) {
	// Kanonik sıra: rota sırasına göre gruplu, grup içinde girdi sırası.
	set := yonSet(
		ProxyRedirect{From: "www.example.com", To: "example.com"},
		ProxyRedirect{From: "old.example.com", To: "example.com"},
		ProxyRedirect{From: "www.other.example", To: "other.example"},
	)
	cfg, err := FixedConfig(set)
	if err != nil {
		t.Fatal(err)
	}
	got, err := SafeRoutes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, set) {
		t.Fatalf("gidiş-dönüş farklı:\n got %+v\nwant %+v", got, set)
	}
}

func TestSafeRoutesRejectsTamperedRedirect(t *testing.T) {
	loc := func(v ...string) func(h *Handler) {
		return func(h *Handler) { h.Headers["Location"] = v }
	}
	const ok = "https://example.com{http.request.uri}"
	tamper := map[string]func(h *Handler){
		"gövde":              func(h *Handler) { h.Body = "x" },
		"ikinci başlık":      func(h *Handler) { h.Headers["Set-Cookie"] = []string{"a=b"} },
		"yer tutucu":         loc("https://example.com/{file./etc/passwd}"),
		"durum kodu":         func(h *Handler) { h.StatusCode = 302 },
		"iki Location":       loc(ok, ok),
		"şemasız":            loc("http://example.com{http.request.uri}"),
		"başka hedef":        loc("https://evil.example{http.request.uri}"),
		"sonek yok":          loc("https://example.com"),
		"küçük harf anahtar": func(h *Handler) { h.Headers = map[string][]string{"location": {ok}} },
	}
	for name, mutate := range tamper {
		t.Run(name, func(t *testing.T) {
			cfg, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "example.com"}))
			if err != nil {
				t.Fatal(err)
			}
			routes := cfg.Apps.HTTP.Servers[serverName].Routes
			mutate(&routes[len(routes)-1].Handle[0])
			if _, err := SafeRoutes(cfg); err == nil {
				t.Fatal("kurcalanmış yönlendirme kabul edildi")
			}
		})
	}
}

func TestSafeRoutesRejectsRedirectWithExtraHandler(t *testing.T) {
	cfg, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	routes := cfg.Apps.HTTP.Servers[serverName].Routes
	last := &routes[len(routes)-1]
	last.Handle = append(last.Handle, Handler{Handler: "static_response", StatusCode: 200, Body: "{file./x}"})
	if _, err := SafeRoutes(cfg); err == nil {
		t.Fatal("ek işleyici kabul edildi")
	}
}

func TestRouteSetWithoutRedirectsKeepsWireFormat(t *testing.T) {
	b, err := json.Marshal(yonSet())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "redirects") {
		t.Fatalf("yönlendirmesiz küme redirects anahtarı taşıyor: %s", b)
	}
	const want = `{"routes":[{"domain":"example.com","upstreams":[{"ip":"172.18.0.3","port":8080}]},{"domain":"other.example","upstreams":[{"ip":"172.18.0.4","port":8080}]}]}`
	if string(b) != want {
		t.Fatalf("tel biçimi değişti:\n got %s\nwant %s", b, want)
	}
}

func TestLiveRedirects(t *testing.T) {
	cfg, err := FixedConfig(yonSet(
		ProxyRedirect{From: "www.example.com", To: "example.com"},
		ProxyRedirect{From: "old.example.com", To: "example.com"},
		ProxyRedirect{From: "www.other.example", To: "other.example"},
	))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := LiveRedirects(cfg, "example.com"), []string{"www.example.com", "old.example.com"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("example.com: %v, beklenen %v", got, want)
	}
	if got := LiveRedirects(cfg, "none.example"); len(got) != 0 {
		t.Fatalf("bilinmeyen hedef: %v", got)
	}
	if got := LiveRedirects(nil, "example.com"); len(got) != 0 {
		t.Fatalf("nil yapılandırma: %v", got)
	}
}

func TestLiveRedirectsIgnoresMalformedRoutes(t *testing.T) {
	cfg, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	routes := cfg.Apps.HTTP.Servers[serverName].Routes
	routes[len(routes)-1].Handle[0].Body = "x" // kurcalanmış: güvenilmez
	if got := LiveRedirects(cfg, "example.com"); len(got) != 0 {
		t.Fatalf("kurcalanmış rota taşındı: %v", got)
	}
}

func TestVerifyAppliedDetectsRedirectTargetDrift(t *testing.T) {
	want, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "example.com"}))
	if err != nil {
		t.Fatal(err)
	}
	live, err := FixedConfig(yonSet(ProxyRedirect{From: "www.example.com", To: "other.example"}))
	if err != nil {
		t.Fatal(err)
	}
	if err := Matches(want, live); err == nil {
		t.Fatal("yönlendirme hedefi farkı yakalanmalıydı")
	}
	if err := Matches(want, want); err != nil {
		t.Fatal(err)
	}
}

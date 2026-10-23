package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
)

// ── kadran domain check (K-128 B) ────────────────────────────────────
//
// Gerçek bir yerel TLS sunucusu (httptest; sertifikası "example.com" adını
// kapsıyor) ve gerçek bir HTTP sunucusu. Alan adının 80/443'ü bağlantı
// kancasıyla onlara yönlendiriliyor; DNS sahte. Kanıtlanan: her denetim
// gerçek bir el sıkışmadan geçiyor ve sorun çıkış koduna yansıyor.

type domainFixture struct {
	https, http *httptest.Server
}

func newDomainFixture(t *testing.T) domainFixture {
	t.Helper()
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com"+r.URL.Path, http.StatusPermanentRedirect)
	}))
	t.Cleanup(tlsSrv.Close)
	t.Cleanup(httpSrv.Close)
	return domainFixture{https: tlsSrv, http: httpSrv}
}

// domainTestCLI: example.com sunucuyu (203.0.113.10) gösteriyor; :443 TLS
// sunucusuna, :80 HTTP sunucusuna gidiyor. closed içindeki portlar
// bağlantıyı reddediyor.
func domainTestCLI(t *testing.T, f domainFixture, trusted bool, closed ...string) (*cli, *strings.Builder) {
	t.Helper()
	c, _, _, _ := dnsTestCLI(cliResolver{"ip4 example.com": {"203.0.113.10"}})
	var d net.Dialer
	c.dialNet = func(ctx context.Context, network, addr string) (net.Conn, error) {
		_, port, _ := net.SplitHostPort(addr)
		for _, p := range closed {
			if p == port {
				return nil, &net.OpError{Op: "dial", Net: network, Err: syscall.ECONNREFUSED}
			}
		}
		target := strings.TrimPrefix(f.http.URL, "http://")
		if port == "443" {
			target = strings.TrimPrefix(f.https.URL, "https://")
		}
		return d.DialContext(ctx, network, target)
	}
	if trusted {
		pool := x509.NewCertPool()
		pool.AddCert(f.https.Certificate())
		c.tlsRoots = pool
	} else {
		c.tlsRoots = x509.NewCertPool() // boş: hiçbir şeye güvenmiyor
	}
	out := &strings.Builder{}
	c.stdout = out
	return c, out
}

func TestDomainCheckAllGood(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitOK {
		t.Fatalf("çıkış %d, beklenen 0\n%s", code, out)
	}
	for _, want := range []string{"✓ DNS", "✓ 80/tcp", "✓ 443/tcp", "308", "✓ certificate", "days left"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("çıktıda %q yok:\n%s", want, out)
		}
	}
}

// Güvenilmeyen sertifika (ör. Caddy henüz alamamış ya da başka bir sitenin
// sertifikası): hata ve SUNULAN sertifikanın kimin olduğu görünmeli.
func TestDomainCheckUntrustedCertificate(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, false)

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitError {
		t.Fatalf("çıkış %d, beklenen %d\n%s", code, exitError, out)
	}
	for _, want := range []string{"✗ certificate", "served", "example.com"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("çıktıda %q yok:\n%s", want, out)
		}
	}
}

// Kapalı 80 (K-121'deki GCP güvenlik duvarı): HTTP-01 doğrulaması
// yapılamaz.
func TestDomainCheckClosedPort80(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true, "80")

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitError {
		t.Fatalf("çıkış %d, beklenen %d\n%s", code, exitError, out)
	}
	if !strings.Contains(out.String(), "✗ 80/tcp") {
		t.Errorf("kapalı 80 bildirilmedi:\n%s", out)
	}
	// Diğer denetimler yine koşmalı: tanı komutu ilk hatada durmaz.
	if !strings.Contains(out.String(), "✓ certificate") {
		t.Errorf("80 kapalıyken sertifika denetimi atlandı:\n%s", out)
	}
}

// DNS başka yeri gösteriyorsa bu bir sorun; tanı yine sürer.
func TestDomainCheckWrongDNS(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}}

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitError || !strings.Contains(out.String(), "✗ DNS") {
		t.Fatalf("yanlış DNS bildirilmedi (çıkış %d):\n%s", code, out)
	}
	// Sonraki ✓ satırları başka bir sunucuyu ölçüyor; okuyan bunu bilmeli.
	if !strings.Contains(out.String(), "points at now") {
		t.Errorf("yanlış DNS'te diğer satırların başka sunucuyu ölçtüğü söylenmedi:\n%s", out)
	}
}

// Yetkili doğru, yerel önbellek eski: uyarı satırı komut önermez. `-wait`
// yalnız Stop'ta bekler; önerilen `kadran domain check -wait 10m` (alan adı
// ve hedefle bile) beklemeden dönüyordu (23 Eki).
func TestDomainCheckStaleCacheSuggestsNoCommand(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{
		"ip4 example.com":        {"198.51.100.7"},
		"ip4 ns1.vercel-dns.com": {"192.0.2.1"},
	}
	c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"203.0.113.10"}, TTL: 600}})

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitOK {
		t.Fatalf("yalnız önbellek eskiyken çıkış %d, beklenen %d:\n%s", code, exitOK, out)
	}
	const want = "  ! DNS       your DNS host (Vercel) already answers 203.0.113.10; this machine still sees 198.51.100.7 " +
		"from its cache (the new record's TTL is 600 s, how long the old answer stays cached is unknown). " +
		"Let's Encrypt usually sees the new record\n"
	if !strings.Contains(out.String(), want) {
		t.Errorf("DNS satırı %q değil:\n%s", want, out)
	}
	if strings.Contains(out.String(), "-wait") {
		t.Errorf("uyarı beklemeyen bir -wait komutu öneriyor:\n%s", out)
	}
}

// Otomatik yenilenen bir sertifikada 7 günden az kalması yenilemenin
// işlemediğini gösterir. Gerçek, kısa ömürlü (3 gün) bir sertifikayla.
func TestDomainCheckExpiringCertificate(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "example.com"},
		DNSNames:     []string{"example.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(3 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	f := newDomainFixture(t)
	f.https = srv
	c, out := domainTestCLI(t, f, false)
	c.tlsRoots.AddCert(leaf)

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitError || !strings.Contains(out.String(), "✗ certificate") || !strings.Contains(out.String(), "renewal") {
		t.Fatalf("bitmek üzere olan sertifika bildirilmedi (çıkış %d):\n%s", code, out)
	}
}

func TestDomainCheckUsage(t *testing.T) {
	c, _, stderr := newTestCLI("")
	if code := c.runDomain(t.Context(), []string{"check"}); code != exitUsage {
		t.Fatalf("çıkış %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("kullanım metni yok: %q", stderr)
	}
}

// Kayıt firması satırı: DNS başka firmadaysa kaydın NEREDE değişeceğini
// açıkça söyler (K-151).
func TestDomainCheckShowsTheRegistrar(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}, "ip4 ns1.vercel-dns.com": {"192.0.2.1"}}
	c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"198.51.100.7"}}})

	mux := http.NewServeMux()
	rdap := httptest.NewTLSServer(mux)
	t.Cleanup(rdap.Close)
	mux.HandleFunc("/boot.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"services":[[["com"],["` + rdap.URL + `/"]]]}`))
	})
	mux.HandleFunc("/domain/example.com", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Name.com, Inc."]]]}]}`))
	})
	c.rdapClient = rdap.Client()
	c.rdapBootstrap = rdap.URL + "/boot.json"

	_ = c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	for _, want := range []string{"registrar", "Name.com, Inc.", "the DNS is at Vercel: change the record there"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q yok:\n%s", want, out)
		}
	}
}

// attachRegistry: example.com icin bir RDAP sunucusu (TLS) kurar ve cli'ye
// baglar. domain nil ise domain/<ad> istegi 404 doner.
func attachRegistry(t *testing.T, c *cli, domain http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	rdap := httptest.NewTLSServer(mux)
	t.Cleanup(rdap.Close)
	mux.HandleFunc("/boot.json", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"services":[[["com"],["` + rdap.URL + `/"]]]}`))
	})
	if domain != nil {
		mux.HandleFunc("/domain/example.com", domain)
	}
	c.rdapClient = rdap.Client()
	c.rdapBootstrap = rdap.URL + "/boot.json"
	return rdap
}

// countConns: sunucuya acilan TCP baglanti sayisini sayar.
func countConns(srv *httptest.Server) *int {
	n := 0
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			n++
		}
	}
	return &n
}

// RDAP asla tanıyı bozmaz (T8a): kayit otoritesi takilsa bile DNS dogruysa
// cikis 0 kalir, hicbir ✗ satiri cikmaz ve kayit firmasi satiri basilmaz.
func TestDomainCheckSurvivesAHangingRegistry(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	mux := http.NewServeMux()
	hang := httptest.NewTLSServer(mux)
	t.Cleanup(hang.Close)
	mux.HandleFunc("/boot.json", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c.rdapClient = hang.Client()
	c.rdapBootstrap = hang.URL + "/boot.json"

	code := c.runDomain(t.Context(), []string{"check", "--timeout", "2s", "example.com", "kimse@203.0.113.10"})

	if code != exitOK {
		t.Fatalf("çıkış %d, beklenen %d\n%s", code, exitOK, out)
	}
	if strings.Contains(out.String(), "✗") {
		t.Errorf("takılan kayıt otoritesi tanıyı bozdu:\n%s", out)
	}
	if strings.Contains(out.String(), "accredited registrar") {
		t.Errorf("zaman aşımında kayıt firması satırı basıldı:\n%s", out)
	}
}

// Bilinmeyen DNS saglayicisi: kayit firmasi satiri yine basilir ama "DNS
// su yerde" ipucu eklenmez.
func TestDomainCheckRegistrarWithoutKnownDNSProvider(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}}
	attachRegistry(t, c, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"entities":[{"roles":["registrar"],"vcardArray":["vcard",[["fn",{},"text","Name.com, Inc."]]]}]}`))
	})

	_ = c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if !strings.Contains(out.String(), "Name.com, Inc.") {
		t.Errorf("kayıt firması satırı yok:\n%s", out)
	}
	if strings.Contains(out.String(), "the DNS is at") {
		t.Errorf("bilinmeyen sağlayıcıda DNS ipucu basıldı:\n%s", out)
	}
}

// Kayit otoritesi 500 donerse satir yok, cikis kodu etkilenmez.
func TestDomainCheckRegistryErrorIsSilent(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	attachRegistry(t, c, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitOK {
		t.Fatalf("çıkış %d, beklenen %d\n%s", code, exitOK, out)
	}
	if strings.Contains(out.String(), "accredited registrar") {
		t.Errorf("hata cevabında kayıt firması satırı basıldı:\n%s", out)
	}
}

// Atlanan (.localhost) alan adinda kayit otoritesine HIC istek gitmez.
func TestDomainCheckSkippedNameAsksNoRegistry(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	rdap := attachRegistry(t, c, nil)
	conns := countConns(rdap)

	_ = c.runDomain(t.Context(), []string{"check", "app.localhost", "kimse@203.0.113.10"})

	if *conns != 0 {
		t.Errorf("atlanan alan adı için kayıt otoritesine %d bağlantı açıldı:\n%s", *conns, out)
	}
}

// c.resolver != nil koruması testleri agdan uzak tutar: rdapClient nil iken
// onyukleme sunucusuna hic baglanilmaz.
func TestDomainCheckFakeResolverNeverReachesTheNetwork(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	boot := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(boot.Close)
	conns := countConns(boot)
	c.rdapBootstrap = boot.URL + "/boot.json" // rdapClient bilerek nil

	_ = c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if *conns != 0 {
		t.Errorf("sahte çözücüyle %d bağlantı açıldı:\n%s", *conns, out)
	}
}

// switchingResolver: ilk n sorguda wrong, sonra right döner.
type switchingResolver struct {
	n            *int
	after        int
	wrong, right cliResolver
}

func (s switchingResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if network == "ip4" && host == "example.com" {
		*s.n++
	}
	if *s.n <= s.after {
		return s.wrong.LookupIP(ctx, network, host)
	}
	return s.right.LookupIP(ctx, network, host)
}

// pollResolver: example.com A sorgusunun k. turunda ips[k] döner; son
// adres tekrarlanır.
type pollResolver struct {
	polls *int
	ips   []string
}

func (p pollResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	if network == "ip4" && host == "example.com" {
		*p.polls++
	}
	i := min(*p.polls, len(p.ips)) - 1
	return cliResolver{"ip4 example.com": {p.ips[i]}}.LookupIP(ctx, network, host)
}

// assertWaitIntervals, bekleme sürelerinin sayısını ve her birinin
// dnsWaitInterval olduğunu denetler.
func assertWaitIntervals(t *testing.T, got []time.Duration, want int) {
	t.Helper()
	if len(got) != want {
		t.Fatalf("bekleme sayısı %d, beklenen %d", len(got), want)
	}
	for i, d := range got {
		if d != dnsWaitInterval {
			t.Errorf("%d. bekleme %s, beklenen %s", i+1, d, dnsWaitInterval)
		}
	}
}

func TestDnsWaitIntervalIsFifteenSeconds(t *testing.T) {
	if dnsWaitInterval != 15*time.Second {
		t.Fatalf("dnsWaitInterval %s, beklenen 15s", dnsWaitInterval)
	}
}

func TestDomainCheckWaitsUntilTheDNSIsRight(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	n := 0
	c.resolver = switchingResolver{n: &n, after: 2,
		wrong: cliResolver{"ip4 example.com": {"198.51.100.7"}},
		right: cliResolver{"ip4 example.com": {"203.0.113.10"}}}
	var sleeps []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }

	code := c.runDomain(t.Context(), []string{"check", "-wait", "10m", "example.com", "kimse@203.0.113.10"})

	if code != exitOK {
		t.Fatalf("çıkış %d (beklenen 0):\n%s", code, out)
	}
	assertWaitIntervals(t, sleeps, 2)
	if got := strings.Count(out.String(), "waiting for DNS"); got != 1 {
		t.Errorf("aynı yanlış cevaptan %d bekleme satırı çıktı (beklenen 1):\n%s", got, out)
	}
}

func TestDomainCheckWaitPrintsAgainWhenTheReasonChanges(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	polls := 0
	c.resolver = pollResolver{polls: &polls,
		ips: []string{"198.51.100.7", "198.51.100.8", "203.0.113.10"}}
	var sleeps []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }

	code := c.runDomain(t.Context(), []string{"check", "-wait", "10m", "example.com", "kimse@203.0.113.10"})

	if code != exitOK {
		t.Fatalf("çıkış %d (beklenen 0):\n%s", code, out)
	}
	assertWaitIntervals(t, sleeps, 2)
	if got := strings.Count(out.String(), "waiting for DNS"); got != 2 {
		t.Errorf("neden değişince %d bekleme satırı (beklenen 2):\n%s", got, out)
	}
}

func TestDomainCheckWaitGivesUp(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}}
	var sleeps []time.Duration
	c.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		if len(sleeps) == 3 {
			return context.DeadlineExceeded
		}
		return nil
	}

	code := c.runDomain(t.Context(), []string{"check", "-wait", "1m", "example.com", "kimse@203.0.113.10"})

	if code != exitError || !strings.Contains(out.String(), "✗ DNS") {
		t.Fatalf("süre dolunca hata beklenirdi (çıkış %d):\n%s", code, out)
	}
	assertWaitIntervals(t, sleeps, 3)
}

func TestDomainCheckWaitInterruptedByCancel(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{"ip4 example.com": {"198.51.100.7"}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	code := c.runDomain(ctx, []string{"check", "-wait", "10m", "example.com", "kimse@203.0.113.10"})

	if code != exitError || !strings.Contains(out.String(), "interrupted") {
		t.Fatalf("iptal kesintisi beklenirdi (çıkış %d):\n%s", code, out)
	}
	if strings.Contains(out.String(), "80/tcp") {
		t.Errorf("iptal sonrası sonda çalışmamalı:\n%s", out)
	}
}

func TestDomainCheckNegativeWaitIsUsageError(t *testing.T) {
	f := newDomainFixture(t)
	c, _ := domainTestCLI(t, f, true)

	code := c.runDomain(t.Context(), []string{"check", "-wait=-5m", "example.com", "kimse@203.0.113.10"})

	if code != exitUsage {
		t.Fatalf("çıkış %d, beklenen %d (kullanım hatası)", code, exitUsage)
	}
}

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
)

// ── panely domain check (K-128 B) ────────────────────────────────────
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
	for _, want := range []string{"✓ DNS", "✓ 80/tcp", "✓ 443/tcp", "308", "✓ sertifika", "gün"} {
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
	for _, want := range []string{"✗ sertifika", "sunulan", "example.com"} {
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
	if !strings.Contains(out.String(), "✓ sertifika") {
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
	if !strings.Contains(out.String(), "şu an gösterdiği sunucuya") {
		t.Errorf("yanlış DNS'te diğer satırların başka sunucuyu ölçtüğü söylenmedi:\n%s", out)
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

	if code != exitError || !strings.Contains(out.String(), "✗ sertifika") || !strings.Contains(out.String(), "yenileme") {
		t.Fatalf("bitmek üzere olan sertifika bildirilmedi (çıkış %d):\n%s", code, out)
	}
}

func TestDomainCheckUsage(t *testing.T) {
	c, _, stderr := newTestCLI("")
	if code := c.runDomain(t.Context(), []string{"check"}); code != exitUsage {
		t.Fatalf("çıkış %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(stderr.String(), "kullanım") {
		t.Errorf("kullanım metni yok: %q", stderr)
	}
}

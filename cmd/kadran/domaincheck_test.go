package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/client"
	panelyv1 "github.com/erkanrzgc/kadran/internal/pb/panely/v1"
)

// ── Alan adı önkontrolü (K-128) ──────────────────────────────────────
//
// Sahte çözücü ve sahte bağlantıyla: gerçek DNS CI'da dalgalı, gerçek
// bağlantı burada istenmiyor. Kanıtlanan: yanlış DNS'te bağlantı HİÇ
// denenmiyor; `-skip-dns-check` bağlantıya ulaşıyor; rapor stdout'a değil
// stderr'e gidiyor (-json bozulmasın).

type cliResolver map[string][]string

func (f cliResolver) LookupIP(_ context.Context, network, host string) ([]net.IP, error) {
	ips, ok := f[network+" "+host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	out := make([]net.IP, 0, len(ips))
	for _, s := range ips {
		out = append(out, net.ParseIP(s))
	}
	return out, nil
}

var errDialedForTest = errors.New("test: bağlantı denendi")

// dnsTestCLI, sahte çözücülü ve bağlantı denemelerini sayan bir cli.
func dnsTestCLI(res cliResolver) (c *cli, stdout, stderr *bytes.Buffer, dials *int) {
	c, stdout, stderr = newTestCLI("")
	n := 0
	c.resolver = res
	c.sshHostname = func(_ context.Context, host string) (string, error) { return host, nil }
	c.dial = func(context.Context, string) (*client.Client, *panelyv1.PingResponse, error) {
		n++
		return nil, nil, errDialedForTest
	}
	return c, stdout, stderr, &n
}

var wrongDNS = cliResolver{"ip4 app.example.com": {"198.51.100.7"}}

func TestAppCreateStopsOnWrongDNSBeforeConnecting(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(wrongDNS)

	code := c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if code != exitError {
		t.Fatalf("çıkış kodu %d, beklenen %d\n%s", code, exitError, stderr)
	}
	if *dials != 0 {
		t.Fatalf("yanlış DNS'e rağmen sunucuya bağlanıldı (%d kez)", *dials)
	}
	for _, want := range []string{"198.51.100.7", "203.0.113.10", "-skip-dns-check"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("hata %q içermiyor:\n%s", want, stderr)
		}
	}
}

func TestAppCreateSkipFlagReachesTheConnection(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(wrongDNS)

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "-skip-dns-check", "web", "kimse@203.0.113.10"})

	if *dials != 1 {
		t.Fatalf("-skip-dns-check verildi ama bağlantı denenmedi (%d)\n%s", *dials, stderr)
	}
}

func TestAppCreateWithMatchingDNSConnects(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(cliResolver{"ip4 app.example.com": {"203.0.113.10"}})

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if *dials != 1 {
		t.Fatalf("doğru DNS'te bağlantı denenmedi (%d)\n%s", *dials, stderr)
	}
}

func TestAppUpdateStopsOnWrongDNSBeforeConnecting(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(wrongDNS)

	code := c.runAppUpdate(t.Context(), []string{"-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if code != exitError || *dials != 0 {
		t.Fatalf("çıkış %d, bağlantı %d; beklenen hata ve 0 bağlantı\n%s", code, *dials, stderr)
	}
}

// `-domain=""` uygulamayı vekilden çıkarır; denetlenecek ad yok.
func TestAppUpdateRemovingTheDomainIsNotChecked(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(wrongDNS)

	_ = c.runAppUpdate(t.Context(), []string{"-domain=", "web", "kimse@203.0.113.10"})

	if *dials != 1 {
		t.Fatalf("alan adı kaldırılırken bağlantı denenmedi (%d)\n%s", *dials, stderr)
	}
}

// Canlıdaki `pf.localhost` ve `hello.localhost` rotaları güncellenebilmeli.
func TestLocalhostDomainIsNotBlocked(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(cliResolver{})

	_ = c.runAppUpdate(t.Context(), []string{"-domain", "hello.localhost", "web", "kimse@203.0.113.10"})

	if *dials != 1 {
		t.Fatalf(".localhost engellendi (%d)\n%s", *dials, stderr)
	}
}

// Belirsiz durumda (sunucu özel ağda) devam edilir; uyarı stderr'e gider,
// stdout'a tek bayt yazılmaz (-json bozulmasın).
func TestDNSWarningGoesToStderrOnly(t *testing.T) {
	c, stdout, stderr, dials := dnsTestCLI(wrongDNS)

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "-json", "web", "kimse@10.0.0.5"})

	if *dials != 1 {
		t.Fatalf("belirsiz durumda bağlantı denenmedi (%d)\n%s", *dials, stderr)
	}
	if !strings.Contains(stderr.String(), "genel adresi bilinmiyor") {
		t.Errorf("uyarı stderr'de yok:\n%s", stderr)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout'a yazıldı, -json bozulur: %q", stdout)
	}
}

// `ssh -G` çıktısı: Windows'ta satır sonu \r\n; `hostnamex` gibi başka
// anahtarlar eşleşmemeli; satır yoksa hata.
func TestParseSSHHostname(t *testing.T) {
	out := []byte("user kimse\r\nhostnamex yanlis\r\nhostname 203.0.113.10\r\nport 22\r\n")
	if got, err := parseSSHHostname(out); err != nil || got != "203.0.113.10" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := parseSSHHostname([]byte("user kimse\nport 22\n")); err == nil {
		t.Fatal("hostname satırı yokken hata dönmedi")
	}
}

// ssh yapılandırmasındaki takma ad gerçek ada çevrilmeli: `prod` diye
// bağlanan kullanıcının sunucusu ancak ssh'ın HostName eşlemesiyle bilinir.
func TestSSHAliasIsResolvedThroughSSHConfig(t *testing.T) {
	c, _, stderr, dials := dnsTestCLI(cliResolver{"ip4 app.example.com": {"203.0.113.10"}})
	c.sshHostname = func(_ context.Context, host string) (string, error) {
		if host == "prod" {
			return "203.0.113.10", nil
		}
		return host, nil
	}

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@prod"})

	if *dials != 1 || strings.Contains(stderr.String(), "bilinmiyor") {
		t.Fatalf("takma ad çözülmedi (bağlantı %d)\n%s", *dials, stderr)
	}
}

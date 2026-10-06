package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/domaincheck"
)

// ── kadran domain check (K-128 B) ────────────────────────────────────
//
// "Sertifika neden gelmiyor?" sorusunun tanısı, kullanıcının makinesinden:
// DNS, 80 ve 443'e TCP, 80'de HTTP yanıtı ve 443'te sertifika. Sunucuya
// RPC yok; ilk hatada durmaz, hepsini raporlar.

// domainProbeTimeout, tek bir bağlantı ya da isteğin süre sınırı.
const domainProbeTimeout = 5 * time.Second

// certExpiryFailDays: otomatik yenilenen bir sertifikada bundan az gün
// kalması, yenilemenin işlemediğini gösterir.
const certExpiryFailDays = 7

// domainReport, ✓/✗/! satırlarını yazar ve bir hata olup olmadığını tutar.
type domainReport struct {
	out    io.Writer
	failed bool
}

func (r *domainReport) ok(label, detail string)   { r.line("✓", label, detail) }
func (r *domainReport) warn(label, detail string) { r.line("!", label, detail) }
func (r *domainReport) fail(label, detail string) {
	r.failed = true
	r.line("✗", label, detail)
}

func (r *domainReport) line(mark, label, detail string) {
	fmt.Fprintf(r.out, "  %s %-9s %s\n", mark, label, detail)
}

func (c *cli) runDomain(ctx context.Context, args []string) int {
	if len(args) == 0 || args[0] != "check" {
		return c.usageError("usage: kadran domain check <domain> [target]")
	}
	return c.runDomainCheck(ctx, args[1:])
}

func (c *cli) runDomainCheck(ctx context.Context, args []string) int {
	fs := c.newFlagSet("domain check")
	timeout := fs.Duration("timeout", 30*time.Second, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("usage: kadran domain check <domain> [target]")
	}
	domain := strings.TrimSuffix(strings.ToLower(fs.Arg(0)), ".")
	if strings.ContainsAny(domain, "/: ") || domain == "" {
		return c.usageError("invalid domain (%q) — it cannot contain a scheme, port or path", fs.Arg(0))
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	fmt.Fprintf(c.stdout, "Domain: %s\n", domain)
	r := &domainReport{out: c.stdout}
	if c.probeDNS(ctx, r, domain, fs.Arg(1)) == domaincheck.Stop {
		// Yanlış DNS'te sonraki satırlar BU sunucuyu değil, alan adının
		// gösterdiği yeri ölçüyor (ör. joker kayıtta Vercel'in sertifikası).
		fmt.Fprintln(c.stdout, "    the checks below hit the server the domain points at now, not this server")
	}
	c.probePorts(ctx, r, domain)
	c.probeHTTP(ctx, r, domain)
	c.probeTLS(ctx, r, domain)
	if r.failed {
		return exitError
	}
	return exitOK
}

func (c *cli) probeDNS(ctx context.Context, r *domainReport, domain, rawTarget string) domaincheck.Verdict {
	server, ok := c.serverHostFor(ctx, rawTarget)
	if !ok {
		r.warn("DNS", "could not parse the target; the server's address is unknown")
	}
	rep := domaincheck.Check(ctx, c.dnsResolver(), domain, server)
	records := joinAddrs(append(rep.A, rep.AAAA...))
	switch rep.Verdict {
	case domaincheck.OK:
		r.ok("DNS", records+" (server)")
	case domaincheck.Skipped, domaincheck.Warn:
		r.warn("DNS", strings.Join(rep.Reasons, "; "))
	default:
		r.fail("DNS", strings.Join(rep.Reasons, "; "))
	}
	return rep.Verdict
}

func (c *cli) netDial(ctx context.Context, network, addr string) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, domainProbeTimeout)
	defer cancel()
	if c.dialNet != nil {
		return c.dialNet(ctx, network, addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, network, addr)
}

func (c *cli) probePorts(ctx context.Context, r *domainReport, domain string) {
	for _, p := range []struct{ port, why string }{
		{"80", "needed for Let's Encrypt's HTTP validation and the HTTPS redirect"},
		{"443", "needed for HTTPS and TLS-ALPN validation"},
	} {
		conn, err := c.netDial(ctx, "tcp", net.JoinHostPort(domain, p.port))
		if err != nil {
			r.fail(p.port+"/tcp", fmt.Sprintf("could not connect (%v) — %s; check the firewall", err, p.why))
			continue
		}
		_ = conn.Close()
		r.ok(p.port+"/tcp", "open")
	}
}

func (c *cli) probeHTTP(ctx context.Context, r *domainReport, domain string) {
	hc := &http.Client{
		Transport: &http.Transport{DialContext: c.netDial, Proxy: nil},
		// Yönlendirme izlenmiyor: yanıtın kendisi ölçülüyor.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       domainProbeTimeout,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+domain+"/", nil)
	if err != nil {
		r.fail("HTTP", err.Error())
		return
	}
	resp, err := hc.Do(req)
	if err != nil {
		r.fail("HTTP", fmt.Sprintf("no response (%v)", err))
		return
	}
	_ = resp.Body.Close()
	loc := resp.Header.Get("Location")
	switch {
	case resp.StatusCode >= 300 && resp.StatusCode < 400 && strings.HasPrefix(loc, "https://"):
		r.ok("HTTP", fmt.Sprintf("%d → %s", resp.StatusCode, loc))
	default:
		r.warn("HTTP", fmt.Sprintf("%d — does not redirect to HTTPS", resp.StatusCode))
	}
}

func (c *cli) probeTLS(ctx context.Context, r *domainReport, domain string) {
	leaf, err := c.tlsLeaf(ctx, domain, false)
	if err == nil {
		days := int(time.Until(leaf.NotAfter).Hours() / 24)
		detail := fmt.Sprintf("%s, %d days left", issuerName(leaf), days)
		if days < certExpiryFailDays {
			r.fail("certificate", detail+"; automatic renewal may not be working")
			return
		}
		r.ok("certificate", detail)
		return
	}
	served, serr := c.tlsLeaf(ctx, domain, true)
	if serr != nil {
		r.fail("certificate", fmt.Sprintf("handshake failed (%v)", err))
		return
	}
	r.fail("certificate", fmt.Sprintf("not trusted (%v); served: %s, issued by %s, names %s",
		err, served.Subject.CommonName, issuerName(served), strings.Join(served.DNSNames, ", ")))
}

// tlsLeaf, 443'te sunulan sertifikayı döndürür. describe=true yalnızca
// GÜVENİLMEYEN bir sertifikayı TARİF etmek için doğrulamasız el sıkışır;
// bağlantı üzerinden hiçbir veri gönderilmez.
func (c *cli) tlsLeaf(ctx context.Context, domain string, describe bool) (*x509.Certificate, error) {
	conn, err := c.netDial(ctx, "tcp", net.JoinHostPort(domain, "443"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	cfg := &tls.Config{ServerName: domain, RootCAs: c.tlsRoots, MinVersion: tls.VersionTLS12}
	if describe {
		cfg.InsecureSkipVerify = true //nolint:gosec // G402: yalnızca sunulan sertifikayı tarif etmek için; veri gönderilmez
	}
	hctx, cancel := context.WithTimeout(ctx, domainProbeTimeout)
	defer cancel()
	tc := tls.Client(conn, cfg)
	if err := tc.HandshakeContext(hctx); err != nil {
		return nil, err
	}
	certs := tc.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, fmt.Errorf("the server sent no certificate")
	}
	return certs[0], nil
}

func issuerName(cert *x509.Certificate) string {
	name := cert.Issuer.CommonName
	if len(cert.Issuer.Organization) > 0 {
		name = cert.Issuer.Organization[0] + " (" + name + ")"
	}
	return name
}

func joinAddrs(addrs []netip.Addr) string {
	s := make([]string, len(addrs))
	for i, a := range addrs {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}

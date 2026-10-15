package main

import (
	"bytes"
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
)

// cliNS, ada göre NS kayıtları; tanımsız ad "bulunamadı".
type cliNS map[string][]string

func (f cliNS) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
	hosts, ok := f[name]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: name, IsNotFound: true}
	}
	out := make([]*net.NS, 0, len(hosts))
	for _, h := range hosts {
		out = append(out, &net.NS{Host: h})
	}
	return out, nil
}

// vercelCLI: example.com Vercel'de; ad sunucusu 192.0.2.1 authAddrs döner.
func vercelCLI(local cliResolver, authAddrs ...string) (*cli, *bytes.Buffer, *bytes.Buffer, *int) {
	res := cliResolver{"ip4 ns1.vercel-dns.com": {"192.0.2.1"}}
	for k, v := range local {
		res[k] = v
	}
	c, stdout, stderr, dials := dnsTestCLI(res)
	c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: authAddrs, TTL: 600}})
	return c, stdout, stderr, dials
}

func TestAppCreateShowsTheDNSHostAndTheRecord(t *testing.T) {
	c, _, stderr, dials := vercelCLI(cliResolver{"ip4 app.example.com": {"198.51.100.7"}}, "198.51.100.7")

	code := c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if code != exitError || *dials != 0 {
		t.Fatalf("çıkış %d, bağlantı %d\n%s", code, *dials, stderr)
	}
	for _, want := range []string{
		"DNS host  Vercel (ns1.vercel-dns.com)",
		`Delete these records for "app": A/ALIAS → 198.51.100.7`,
		"A     app",
		"203.0.113.10",
		"vercel dns add example.com 'app' A 203.0.113.10",
		"Guide: https://vercel.com/",
		"kadran domain check -wait 10m app.example.com kimse@203.0.113.10",
		"-skip-dns-check",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("hata %q içermiyor:\n%s", want, stderr)
		}
	}
}

// Karışık küme: sunucu adresi zaten cevap veriyor, yanında yabancı bir adres
// var. Yalnız yabancı olan silinir; "kayıt ekle" ve genel öneri basılmaz.
func TestWriteDNSGuideMixedSetOnlyDeletes(t *testing.T) {
	mixed := []string{"203.0.113.10", "198.51.100.7"}
	c, _, stderr, _ := vercelCLI(cliResolver{"ip4 app.example.com": mixed}, mixed...)

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	out := stderr.String()
	for _, want := range []string{
		`Delete these records for "app": A/ALIAS → 198.51.100.7`,
		"If there is no record by that name, a wildcard (*) record is answering for it",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q yok:\n%s", want, out)
		}
	}
	for _, bad := range []string{"Set this record", "Add an A record"} {
		if strings.Contains(out, bad) {
			t.Errorf("karışık kümede %q basılmamalı:\n%s", bad, out)
		}
	}
}

// Yetkili doğru, yerel eski: bağlantı DENENİR ve uyarı stderr'de.
func TestAppCreateContinuesWhenOnlyTheCacheIsStale(t *testing.T) {
	c, stdout, stderr, dials := vercelCLI(cliResolver{"ip4 app.example.com": {"198.51.100.7"}}, "203.0.113.10")

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	if *dials != 1 {
		t.Fatalf("yalnız önbellek eskiyken bağlantı denenmedi (%d)\n%s", *dials, stderr)
	}
	if !strings.Contains(stderr.String(), "already answers 203.0.113.10") || stdout.Len() != 0 {
		t.Errorf("uyarı stderr'de değil ya da stdout kirlendi:\nstderr=%s\nstdout=%s", stderr, stdout)
	}
}

func TestWriteDNSGuideForProxiedCloudflare(t *testing.T) {
	c, _, stderr, _ := dnsTestCLI(cliResolver{
		"ip4 app.example.com":        {"104.21.3.4"},
		"ip4 adam.ns.cloudflare.com": {"192.0.2.1"},
	})
	c.nsResolver = cliNS{"example.com": {"adam.ns.cloudflare.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"104.21.3.4"}}})

	_ = c.runAppCreate(t.Context(), []string{
		"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})

	for _, want := range []string{"Proxy status: DNS only (grey cloud)", "Full (strict)"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("%q yok:\n%s", want, stderr)
		}
	}
	if strings.Contains(stderr.String(), "Set this record") {
		t.Errorf("proxy'li kayıtta kayıt eklemek önerilmemeli:\n%s", stderr)
	}
}

// Sahte çözümleyiciyle (test) NS ya da yetkili sorgu verilmemişse ağa
// ÇIKILMAZ: CI gerçek DNS'e bağlı olamaz.
func TestDNSCheckerStaysOfflineInTests(t *testing.T) {
	c, _, _, _ := dnsTestCLI(cliResolver{})
	if ch := c.dnsChecker(); ch.NS != nil {
		t.Fatalf("test çözümleyicisiyle NS sorgusu açık")
	}
	c.nsResolver = cliNS{}
	ch := c.dnsChecker()
	if ch.Exchange == nil {
		t.Fatalf("NS verilmiş ama yetkili sorgu gerçek ağa açık kaldı")
	}
	server := netip.AddrPortFrom(netip.MustParseAddr("192.0.2.1"), 53)
	if _, err := ch.Exchange(t.Context(), server, nil); err == nil {
		t.Fatalf("test yetkili sorgusu hata vermedi")
	}
}

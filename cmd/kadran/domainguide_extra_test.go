package main

import (
	"bytes"
	"net/netip"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/domaincheck"
	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
)

// writeDNSGuide'in yazdigi satirlar, agsiz, elle kurulmus raporlarla.
func TestWriteDNSGuideTable(t *testing.T) {
	prov := domaincheck.Provider{Name: "Acme", Panel: "Acme panel", Guide: "https://acme.example/help"}
	rec := func(typ, name, value string) domaincheck.Record {
		return domaincheck.Record{Type: typ, Name: name, Value: value}
	}
	cases := []struct {
		name    string
		rep     domaincheck.Report
		want    []string
		notWant []string
	}{
		{
			name: "AAAA ve A ayni adda, ilk gorulme sirasiyla",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, Fix: domaincheck.Fix{Remove: []domaincheck.Record{
				rec("AAAA", "app", "2001:db8::bad"), rec("A", "app", "198.51.100.7"), rec("A", "app", "198.51.100.8")}}},
			want:    []string{`Delete these records for "app": AAAA → 2001:db8::bad; A/ALIAS → 198.51.100.7, 198.51.100.8`, "wildcard (*)"},
			notWant: []string{"Add an A record"},
		},
		{
			name: "yalniz AAAA",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, Fix: domaincheck.Fix{
				Remove: []domaincheck.Record{rec("AAAA", "@", "2001:db8::bad")}}},
			want: []string{`Delete these records for "@": AAAA → 2001:db8::bad`, "wildcard (*)"},
		},
		{
			name: "CNAME: joker notu yok",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, Fix: domaincheck.Fix{
				Remove: []domaincheck.Record{rec("CNAME", "app", "other.example.net")}}},
			want:    []string{`Delete these records for "app": CNAME → other.example.net`},
			notWant: []string{"wildcard", "A/ALIAS", "Add an A record"},
		},
		{
			name: "iki ad iki satir",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, Fix: domaincheck.Fix{Remove: []domaincheck.Record{
				rec("A", "app", "198.51.100.7"), rec("A", "www", "198.51.100.8")}}},
			want: []string{`for "app": A/ALIAS → 198.51.100.7`, `for "www": A/ALIAS → 198.51.100.8`},
		},
		{
			name:    "Add ve Remove bos: genel oneri",
			rep:     domaincheck.Report{Verdict: domaincheck.Stop},
			want:    []string{"Add an A record pointing the name at the server's public IPv4 address."},
			notWant: []string{"Delete these", "Set this record"},
		},
		{
			name: "Add dolu: kayit blogu, genel oneri yok",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, Fix: domaincheck.Fix{
				Add: []domaincheck.Record{rec("A", "app", "203.0.113.10")}}},
			want:    []string{"Set this record and delete other A, AAAA, ALIAS or CNAME records", "A     app"},
			notWant: []string{"Add an A record pointing"},
		},
		{
			name:    "bolge denetlenmedi, ad sunucusu yok: DNS host satiri yok",
			rep:     domaincheck.Report{Verdict: domaincheck.Stop},
			notWant: []string{"DNS host  "},
		},
		{
			name: "bolge denetlendi, ad sunucusu yok",
			rep:  domaincheck.Report{Verdict: domaincheck.Stop, ZoneChecked: true},
			want: []string{"DNS host  unknown: no nameservers found"},
		},
		{
			// G1: ad cozuluyorsa (ozel son ek, ornegin duckdns.org) "kayitli degil" denmez.
			name: "bolge denetlendi, ad sunucusu yok ama adres var",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, ZoneChecked: true,
				A: []netip.Addr{netip.MustParseAddr("198.51.100.7")}},
			notWant: []string{"DNS host  unknown", "may not be registered"},
		},
		{
			name: "taninmayan firma",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, NameServers: []string{"ns1.unknown.example"},
				Fix: domaincheck.Fix{Add: []domaincheck.Record{rec("A", "app", "203.0.113.10")}}},
			want: []string{"DNS host  ns1.unknown.example", "At your DNS host: control panel",
				"Guide: " + domainsGuideURL},
			notWant: []string{"At Acme"},
		},
		{
			name: "taninan firma",
			rep: domaincheck.Report{Verdict: domaincheck.Stop, HasProvider: true, Provider: prov,
				NameServers: []string{"ns1.acme.example"}},
			want:    []string{"DNS host  Acme (ns1.acme.example)", "At Acme: Acme panel", "Guide: https://acme.example/help"},
			notWant: []string{"At your DNS host"},
		},
		{
			name:    "Stop degilse hicbir sey yazilmaz",
			rep:     domaincheck.Report{Verdict: domaincheck.Warn, ZoneChecked: true},
			notWant: []string{"DNS host  ", "Add an A record"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			writeDNSGuide(&buf, tc.rep)
			for _, w := range tc.want {
				if !strings.Contains(buf.String(), w) {
					t.Errorf("%q yok:\n%s", w, buf.String())
				}
			}
			for _, bad := range tc.notWant {
				if strings.Contains(buf.String(), bad) {
					t.Errorf("%q olmamali:\n%s", bad, buf.String())
				}
			}
		})
	}
}

// `domain check`: tarif stdout'taki rapora (out) yazilir.
func TestDomainCheckWrongDNSShowsTheDNSHost(t *testing.T) {
	f := newDomainFixture(t)
	c, out := domainTestCLI(t, f, true)
	c.resolver = cliResolver{
		"ip4 example.com":        {"198.51.100.7"},
		"ip4 ns1.vercel-dns.com": {"192.0.2.1"},
	}
	c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
	c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"198.51.100.7"}, TTL: 600}})

	code := c.runDomain(t.Context(), []string{"check", "example.com", "kimse@203.0.113.10"})

	if code != exitError {
		t.Fatalf("cikis %d\n%s", code, out)
	}
	for _, want := range []string{"DNS host  Vercel (ns1.vercel-dns.com)", `Delete these records for "@": A/ALIAS → 198.51.100.7`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q yok:\n%s", want, out)
		}
	}
}

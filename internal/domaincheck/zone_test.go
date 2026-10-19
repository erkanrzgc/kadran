package domaincheck

import (
	"context"
	"fmt"
	"net"
	"slices"
	"testing"
)

// fakeNS, ada göre NS kayıtları; tanımsız ad "bulunamadı".
type fakeNS map[string][]string

func (f fakeNS) LookupNS(_ context.Context, name string) ([]*net.NS, error) {
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

// LookupCNAME: varsayılan olarak ad takma ad DEĞİL (kendisine döner).
func (fakeNS) LookupCNAME(_ context.Context, host string) (string, error) {
	return host + ".", nil
}

// aliasNS, bazı adların başka bir ada takma ad (CNAME) olduğu sahte çözümleyici.
// LookupNS takma adın hedefinin NS kaydını döndürür: net.Resolver da CNAME izler.
type aliasNS struct {
	fakeNS
	alias map[string]string
}

func (f aliasNS) LookupCNAME(_ context.Context, host string) (string, error) {
	if target, ok := f.alias[host]; ok {
		return target + ".", nil
	}
	return host + ".", nil
}

// A1: LookupNS CNAME izler; www CNAME @ iken www.example.com bölge sayılmamalı.
func TestFindZoneSkipsAliasNames(t *testing.T) {
	ns := aliasNS{
		fakeNS: fakeNS{
			"example.com":     {"ns1.vercel-dns.com."},
			"www.example.com": {"ns1.vercel-dns.com."},
		},
		alias: map[string]string{"www.example.com": "example.com"},
	}
	z, ok := FindZone(t.Context(), ns, "www.example.com")
	if !ok || z.Name != "example.com" || RecordName("www.example.com", z.Name) != "www" {
		t.Fatalf("bölge %q (%v), kayıt %q; beklenen example.com / www", z.Name, ok, RecordName("www.example.com", z.Name))
	}
}

// Takma ad olmayan, devredilmiş alt bölge hâlâ bölge sayılır.
func TestFindZoneKeepsDelegatedSubzone(t *testing.T) {
	ns := aliasNS{
		fakeNS: fakeNS{
			"example.com":     {"ns1.vercel-dns.com."},
			"sub.example.com": {"ns1.other-dns.example."},
		},
	}
	z, ok := FindZone(t.Context(), ns, "app.sub.example.com")
	if !ok || z.Name != "sub.example.com" {
		t.Fatalf("bölge %q (%v), beklenen sub.example.com", z.Name, ok)
	}
}

func TestFindZone(t *testing.T) {
	ns := fakeNS{
		"example.com":    {"NS2.vercel-dns.com.", "ns1.vercel-dns.com."},
		"co.uk":          {"nsa.nic.uk."},
		"com.tr":         {"ns1.nic.tr."},
		"example.com.tr": {"ns1.natrohost.com."},
		"empty.example":  {},
	}
	tests := []struct {
		domain, zone, record string
		ok                   bool
	}{
		{"example.com", "example.com", "@", true},
		{"kadran.example.com", "example.com", "kadran", true},
		{"a.b.example.com", "example.com", "a.b", true},
		{"www.example.com.tr", "example.com.tr", "www", true},
		// example.co.uk'nın NS'si yok: co.uk bir kayıt otoritesi, kullanıcının
		// bölgesi DEĞİL; yürüyüş oraya tırmanmamalı.
		{"shop.example.co.uk", "", "", false},
		{"nothing.example.org", "", "", false},
	}
	for _, tc := range tests {
		z, ok := FindZone(t.Context(), ns, tc.domain)
		if ok != tc.ok || z.Name != tc.zone {
			t.Errorf("%s: bölge %q (%v), beklenen %q (%v)", tc.domain, z.Name, ok, tc.zone, tc.ok)
			continue
		}
		if ok && RecordName(tc.domain, z.Name) != tc.record {
			t.Errorf("%s: kayıt adı %q, beklenen %q", tc.domain, RecordName(tc.domain, z.Name), tc.record)
		}
	}
	z, _ := FindZone(t.Context(), ns, "example.com")
	if !slices.Equal(z.NameServers, []string{"ns1.vercel-dns.com", "ns2.vercel-dns.com"}) {
		t.Errorf("NS listesi temizlenip sıralanmadı: %v", z.NameServers)
	}
}

func TestFindZoneCapsNameServers(t *testing.T) {
	var hosts []string
	for i := range 20 {
		hosts = append(hosts, fmt.Sprintf("ns%02d.example-dns.net.", i))
	}
	z, ok := FindZone(t.Context(), fakeNS{"example.com": hosts}, "example.com")
	if !ok || len(z.NameServers) != maxNameServers {
		t.Fatalf("%d ad sunucusu kaldı (%v), beklenen %d", len(z.NameServers), ok, maxNameServers)
	}
	if z.NameServers[0] != "ns00.example-dns.net" {
		t.Errorf("kesme sıralamadan SONRA yapılmalı, ilk ad %q", z.NameServers[0])
	}
}

func TestGuessRecordName(t *testing.T) {
	for domain, want := range map[string]string{
		"example.com":        "@",
		"kadran.example.com": "kadran",
		"www.example.com.tr": "www",
		"example.co.uk":      "@",
	} {
		if got := guessRecordName(domain); got != want {
			t.Errorf("guessRecordName(%q) = %q, beklenen %q", domain, got, want)
		}
	}
}

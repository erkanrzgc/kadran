package domaincheck

import (
	"context"
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

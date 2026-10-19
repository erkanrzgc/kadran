package domaincheck

import (
	"strings"
	"testing"
)

// Tablodaki HER son ek kendi firmasını tanıtmalı; büyük harf ve sondaki
// nokta (DNS cevabındaki biçim) fark etmemeli. Test tablodan üretiliyor:
// yeni satır testsiz kalamaz.
func TestEveryProviderSuffixIdentifiesItsProvider(t *testing.T) {
	tried := 0
	for _, p := range providers {
		for _, suf := range p.Suffixes {
			for _, ns := range []string{"ns1." + suf, strings.ToUpper("ns2."+suf) + "."} {
				got, ok := IdentifyProvider([]string{ns})
				if !ok || got.Name != p.Name {
					t.Errorf("%s: tanınan %q (%v), beklenen %q", ns, got.Name, ok, p.Name)
				}
				tried++
			}
		}
	}
	if tried < 100 {
		t.Fatalf("yalnız %d ad denendi — tablo boşalmış olabilir", tried)
	}
}

// L1: Cloudflare ad sunucuları ns3.cloudflare.com gibi adlarla da gelir (canlı
// ölçüm 2026-10-19); yine de etiket sınırı korunmalı.
func TestCloudflareSuffixMatchesOnNameBoundary(t *testing.T) {
	for _, ns := range []string{"ns3.cloudflare.com", "ns4.cloudflare.com", "adam.ns.cloudflare.com"} {
		if p, ok := IdentifyProvider([]string{ns}); !ok || p.Name != providerCloudflare {
			t.Errorf("%s: tanınan %q (%v)", ns, p.Name, ok)
		}
	}
	for _, ns := range []string{"ns1.cloudflare.com.evil.example", "ns1.notcloudflare.com"} {
		if p, ok := IdentifyProvider([]string{ns}); ok {
			t.Errorf("%s yanlışlıkla %q sayıldı", ns, p.Name)
		}
	}
}

func TestAWSPatternMatchesOnlyRoute53(t *testing.T) {
	for _, ns := range []string{"ns-1536.awsdns-00.co.uk", "ns-12.awsdns-01.com", "ns-700.awsdns-23.net", "ns-1200.awsdns-63.org"} {
		if p, ok := IdentifyProvider([]string{ns}); !ok || p.Name != "AWS Route 53" {
			t.Errorf("%s: tanınan %q (%v)", ns, p.Name, ok)
		}
	}
	for _, ns := range []string{"ns-1.awsdns-00.com.evil.example", "awsdns-00.com", "ns-1.awsdns-0.com"} {
		if p, ok := IdentifyProvider([]string{ns}); ok {
			t.Errorf("%s yanlışlıkla %q sayıldı", ns, p.Name)
		}
	}
}

func TestProviderTableIsUsable(t *testing.T) {
	if len(providers) < 50 {
		t.Fatalf("tabloda %d firma var; spec ~50 istiyor", len(providers))
	}
	names := map[string]bool{}
	for _, p := range providers {
		if p.Name == "" || p.Panel == "" {
			t.Errorf("adı ya da panel yolu boş satır: %+v", p)
		}
		if !strings.HasPrefix(p.Guide, "https://") {
			t.Errorf("%s: yardım bağlantısı https değil: %q", p.Name, p.Guide)
		}
		if len(p.Suffixes) == 0 && p.Pattern == "" {
			t.Errorf("%s: hiçbir NS'yi tanıyamaz", p.Name)
		}
		if names[p.Name] {
			t.Errorf("iki satır aynı adı taşıyor: %s", p.Name)
		}
		names[p.Name] = true
	}
}

// İki firmaya bölünmüş, benzer görünen ya da etiket ortasında eşleşen NS
// tanınmamalı: yanlış firma tarifi, hiç tarif vermemekten kötü.
func TestSplitOrLookalikeNameserversAreNotIdentified(t *testing.T) {
	for _, set := range [][]string{
		{"ns1.vercel-dns.com", "adam.ns.cloudflare.com"},
		{"ns1.vercel-dns.com.evil.example"},
		{"ns1.notvercel-dns.com"},
		{"ns.the.net"}, // he.net etiket sınırında eşleşmeli, burada eşleşmemeli
		{"ns1.example.org"},
		{},
	} {
		if p, ok := IdentifyProvider(set); ok {
			t.Errorf("%v tanındı: %s", set, p.Name)
		}
	}
}

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"NS1.Vercel-DNS.com.":     "ns1.vercel-dns.com",
		"ns1.ex\x1b[31mample.com": "ns1.ex31mample.com",
		"ns1 .example.com":        "ns1.example.com",
		"ns1.vercel-dns.com.\x1b": "ns1.vercel-dns.com",
		strings.Repeat("a", 300):  strings.Repeat("a", 253),
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

func TestCommandFor(t *testing.T) {
	v, _ := IdentifyProvider([]string{"ns1.vercel-dns.com"})
	got := v.CommandFor("example.com", Record{Type: "A", Name: "@", Value: "203.0.113.10"})
	if want := `vercel dns add example.com "@" A 203.0.113.10`; got != want {
		t.Errorf("Vercel komutu %q, beklenen %q", got, want)
	}
	do, _ := IdentifyProvider([]string{"ns1.digitalocean.com"})
	if got, want := do.CommandFor("example.com", Record{Type: "A", Name: "@", Value: "203.0.113.10"}),
		`doctl compute domain records create example.com --record-type A --record-name "@" --record-data 203.0.113.10`; got != want {
		t.Errorf("DigitalOcean komutu %q, beklenen %q", got, want)
	}
	// C1: yazdırılan kabuk komutuna yalnız güvenli karakterler girer.
	for _, tc := range []struct{ zone, name, value string }{
		{"example.com", "x'$(id)'", "203.0.113.10"},
		{"example.com", "*", "203.0.113.10"},
		{"example.com", "", "203.0.113.10"},
		{"a b.com", "app", "203.0.113.10"},
		{"", "app", "203.0.113.10"},
		{"example.com", "app", "203.0.113.10; id"},
		{"example.com", "app", "not-an-ip"},
		// C3: "-" ile başlayan etiket komut satırında seçenek olurdu; IPv6 bölge kimliği de.
		{"example.com", "-x", "203.0.113.10"},
		{"example.com", "a.-x", "203.0.113.10"},
		{"-example.com", "app", "203.0.113.10"},
		{"example.com", "app", "fe80::1%eth0"},
	} {
		if cmd := v.CommandFor(tc.zone, Record{Type: "A", Name: tc.name, Value: tc.value}); cmd != "" {
			t.Errorf("CommandFor(%q, %q, %q) = %q, boş bekleniyordu", tc.zone, tc.name, tc.value, cmd)
		}
	}
	cf, _ := IdentifyProvider([]string{"adam.ns.cloudflare.com"})
	if cmd := cf.CommandFor("example.com", Record{Type: "A", Name: "@", Value: "203.0.113.10"}); cmd != "" {
		t.Errorf("komutu olmayan firma komut döndürdü: %q", cmd)
	}
}

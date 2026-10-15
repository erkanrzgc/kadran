package domaincheck

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
	"golang.org/x/net/dns/dnsmessage"
)

// zoneChecker: example.com'un bölgesi ns verilen firmada; iki ad sunucusu
// 192.0.2.1 ve 192.0.2.2. local, yerel çözümleyicinin (önbelleğin) cevabı.
func zoneChecker(nsHosts []string, local fakeResolver, a1, a2 dnstest.Answer) Checker {
	res := fakeResolver{}
	for i, h := range nsHosts {
		// Anahtar TEMİZ ad: bölge yürüyüşü NS adlarını CleanName'den geçiriyor.
		res["ip4 "+CleanName(h)] = answer{ips: []string{[]string{"192.0.2.1", "192.0.2.2"}[i]}}
	}
	for k, v := range local {
		res[k] = v
	}
	return Checker{
		Resolver: res,
		NS:       fakeNS{"example.com": nsHosts},
		Exchange: dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": a1, "192.0.2.2": a2}),
	}
}

var (
	vercelNS     = []string{"ns1.vercel-dns.com.", "ns2.vercel-dns.com."}
	cloudflareNS = []string{"adam.ns.cloudflare.com.", "eve.ns.cloudflare.com."}
	porkbunNS    = []string{"curitiba.ns.porkbun.com.", "fortaleza.ns.porkbun.com."}
)

func auth(addrs ...string) dnstest.Answer {
	return dnstest.Answer{AA: true, Addrs: addrs, TTL: 600}
}

func ip4(name string, ips ...string) fakeResolver {
	return fakeResolver{"ip4 " + name: {ips: ips}}
}

func merge(fs ...fakeResolver) fakeResolver {
	out := fakeResolver{}
	for _, f := range fs {
		for k, v := range f {
			out[k] = v
		}
	}
	return out
}

func TestCheckerRules(t *testing.T) {
	right, wrong := "203.0.113.10", "198.51.100.7"
	tests := []struct {
		name    string
		c       Checker
		domain  string
		server  string
		want    Verdict
		reasons []string // gerekçede geçmesi gereken parçalar
		check   func(t *testing.T, rep Report)
	}{
		{
			name:   "yetkili doğru, yerel önbellek eski: uyarır, durmaz",
			c:      zoneChecker(vercelNS, ip4("app.example.com", wrong), auth(right), auth(right)),
			domain: "app.example.com", server: right,
			want: Warn, reasons: []string{"(Vercel) already answers 203.0.113.10", "600 s", "-wait"},
		},
		{
			name:   "yetkili yanlış, yerel doğru: durur",
			c:      zoneChecker(vercelNS, ip4("app.example.com", right), auth(wrong), auth(wrong)),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{wrong, "own nameservers"},
			check: func(t *testing.T, rep Report) {
				if rep.Zone != "example.com" || rep.RecordName != "app" || !rep.HasProvider || rep.Provider.Name != "Vercel" {
					t.Errorf("bölge/ad/firma: %q %q %v %q", rep.Zone, rep.RecordName, rep.HasProvider, rep.Provider.Name)
				}
				want := []Record{{Type: "A", Name: "app", Value: right}}
				if !slices.Equal(rep.Fix.Add, want) {
					t.Errorf("Fix.Add = %v, beklenen %v", rep.Fix.Add, want)
				}
			},
		},
		{
			name:   "karışık küme: sunucu + eski adres → durur, fazlalık silinecekler listesinde",
			c:      zoneChecker(vercelNS, ip4("app.example.com", right, wrong), auth(right, wrong), auth(right, wrong)),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"(server) and " + wrong},
			check: func(t *testing.T, rep Report) {
				if !slices.Contains(rep.Fix.Remove, Record{Type: "A", Name: "app", Value: wrong}) {
					t.Errorf("Fix.Remove fazlalığı içermiyor: %v", rep.Fix.Remove)
				}
			},
		},
		{
			name:   "karışık küme yerel yolda da durur (NS yok)",
			c:      Checker{Resolver: ip4("app.example.com", right, wrong)},
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"(server) and " + wrong},
		},
		{
			name:   "NS'ler tutarsız: uyarır",
			c:      zoneChecker(vercelNS, ip4("app.example.com", right), auth(right), auth(wrong)),
			domain: "app.example.com", server: right,
			want: Warn, reasons: []string{"do not agree"},
		},
		{
			name: "AA yok (ISS araya girdi): yerel kurallar",
			c: zoneChecker(vercelNS, ip4("app.example.com", wrong),
				dnstest.Answer{Addrs: []string{right}}, dnstest.Answer{Addrs: []string{right}}),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"the A record points at " + wrong},
			check: func(t *testing.T, rep Report) {
				if rep.Auth.Answered {
					t.Errorf("AA bayraksız cevap yetkili sayıldı")
				}
			},
		},
		{
			name: "CNAME hedefi sunucuyu gösteriyor: geçer",
			c: zoneChecker(vercelNS, merge(ip4("www.example.com", right), ip4("target.example.net", right)),
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300},
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300}),
			domain: "www.example.com", server: right,
			want: OK,
		},
		{
			name: "CNAME hedefi başka yer: durur, CNAME silinecekler listesinde",
			c: zoneChecker(vercelNS, merge(ip4("www.example.com", wrong), ip4("target.example.net", wrong)),
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300},
				dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300}),
			domain: "www.example.com", server: right,
			want: Stop, reasons: []string{"CNAME to target.example.net"},
			check: func(t *testing.T, rep Report) {
				if !slices.Contains(rep.Fix.Remove, Record{Type: "CNAME", Name: "www", Value: "target.example.net"}) {
					t.Errorf("Fix.Remove CNAME içermiyor: %v", rep.Fix.Remove)
				}
			},
		},
		{
			name:   "Cloudflare aralığı, NS Cloudflare'de: turuncu bulut",
			c:      zoneChecker(cloudflareNS, ip4("app.example.com", "104.21.3.4"), auth("104.21.3.4"), auth("104.21.3.4")),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"proxied (orange cloud)"},
			check: func(t *testing.T, rep Report) {
				if !rep.Proxied || rep.OnCloudflare || len(rep.Fix.Add) != 0 {
					t.Errorf("Proxied=%v OnCloudflare=%v Fix.Add=%v", rep.Proxied, rep.OnCloudflare, rep.Fix.Add)
				}
			},
		},
		{
			name:   "Cloudflare aralığı, NS Porkbun'da: park sayfası",
			c:      zoneChecker(porkbunNS, ip4("app.example.com", "104.21.3.4"), auth("104.21.3.4"), auth("104.21.3.4")),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"Cloudflare-hosted page"},
			check: func(t *testing.T, rep Report) {
				if rep.Proxied || !rep.OnCloudflare || !slices.Contains(rep.Fix.Remove, Record{Type: "A", Name: "app", Value: "104.21.3.4"}) {
					t.Errorf("Proxied=%v OnCloudflare=%v Fix.Remove=%v", rep.Proxied, rep.OnCloudflare, rep.Fix.Remove)
				}
			},
		},
		{
			name: "yetkili NXDOMAIN, yerelde eski kayıt: durur",
			c: zoneChecker(vercelNS, ip4("app.example.com", right),
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError},
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError}),
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"no A or AAAA record"},
		},
		{
			// İnceleme odağı 1: Vercel'in joker kaydı yeni adı Vercel'e götürüyor.
			name:   "joker kayıt: yeni alt ad Vercel'i gösteriyor",
			c:      zoneChecker(vercelNS, ip4("new.example.com", wrong), auth(wrong), auth(wrong)),
			domain: "new.example.com", server: right,
			want: Stop, reasons: []string{wrong},
			check: func(t *testing.T, rep Report) {
				if rep.RecordName != "new" || !slices.Equal(rep.Fix.Add, []Record{{Type: "A", Name: "new", Value: right}}) {
					t.Errorf("kayıt adı %q, Fix.Add %v", rep.RecordName, rep.Fix.Add)
				}
			},
		},
		{
			// İnceleme odağı 5: yalnız IPv6'sı bilinen sunucuya A önerilmez.
			name: "yalnız IPv6 sunucu: Fix.Add yalnız AAAA",
			c: zoneChecker(vercelNS, fakeResolver{"ip6 sunucu.example": {ips: []string{"2001:db8::10"}}},
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError},
				dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError}),
			domain: "app.example.com", server: "sunucu.example",
			want: Stop,
			check: func(t *testing.T, rep Report) {
				if !slices.Equal(rep.Fix.Add, []Record{{Type: "AAAA", Name: "app", Value: "2001:db8::10"}}) {
					t.Errorf("Fix.Add = %v", rep.Fix.Add)
				}
			},
		},
		{
			name:   "NS kapalı (K-128 yolu): bölge yok, kayıt adı tahmin edilir",
			c:      Checker{Resolver: ip4("app.example.com", wrong)},
			domain: "app.example.com", server: right,
			want: Stop, reasons: []string{"the A record points at " + wrong},
			check: func(t *testing.T, rep Report) {
				if rep.Zone != "" || rep.ZoneChecked || rep.RecordName != "app" {
					t.Errorf("Zone=%q ZoneChecked=%v RecordName=%q", rep.Zone, rep.ZoneChecked, rep.RecordName)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := tc.c.Check(t.Context(), tc.domain, tc.server)
			if rep.Verdict != tc.want {
				t.Fatalf("karar %v, beklenen %v; gerekçe: %q", rep.Verdict, tc.want, rep.Reasons)
			}
			all := strings.Join(rep.Reasons, "\n")
			for _, r := range tc.reasons {
				if !strings.Contains(all, r) {
					t.Errorf("gerekçede %q yok: %q", r, rep.Reasons)
				}
			}
			if tc.check != nil {
				tc.check(t, rep)
			}
		})
	}
}

// Ad sunucusu cevap vermiyor: bütçe dolunca yerel kurallara düşülür,
// komut takılmaz (spec "Süre bütçesi").
func TestCheckerBudgetFallsBackToLocalRules(t *testing.T) {
	block := dnstest.Answer{Block: true}
	c := zoneChecker(vercelNS, ip4("app.example.com", "203.0.113.10"), block, block)
	c.Budget = 200 * time.Millisecond

	start := time.Now()
	rep := c.Check(t.Context(), "app.example.com", "203.0.113.10")
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("bütçe uygulanmadı: %v sürdü", took)
	}
	if rep.Auth.Answered || rep.Verdict != OK {
		t.Fatalf("bütçe aşımında yerel kural bekleniyordu: %+v", rep)
	}
}

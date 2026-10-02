package domaincheck

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

// fakeResolver, ad ve aileye göre önceden verilmiş yanıtları döndürür.
// Tanımlanmamış her sorgu "bulunamadı" (gerçek çözücünün, kaydı olmayan
// aile için verdiği yanıt; Windows'ta ölçüldü, K-128).
type fakeResolver map[string]answer

type answer struct {
	ips []string
	err error
}

func (f fakeResolver) LookupIP(_ context.Context, network, host string) ([]net.IP, error) {
	a, ok := f[network+" "+host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	if a.err != nil {
		return nil, a.err
	}
	out := make([]net.IP, 0, len(a.ips))
	for _, s := range a.ips {
		out = append(out, net.ParseIP(s))
	}
	return out, nil
}

var errServfail = &net.DNSError{Err: "server misbehaving", Name: "x", IsTemporary: true}

func TestCheck(t *testing.T) {
	tests := []struct {
		name   string
		res    fakeResolver
		domain string
		server string
		want   Verdict
		reason string // gerekçede geçmesi gereken parça
	}{
		{
			name:   "A kaydı sunucuyu gösteriyor",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"203.0.113.10"}}},
			domain: "app.example.com", server: "203.0.113.10",
			want: OK,
		},
		{
			name:   "A kaydı başka yeri gösteriyor",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"198.51.100.7"}}},
			domain: "app.example.com", server: "203.0.113.10",
			want: Stop, reason: "198.51.100.7",
		},
		{
			name:   "hiç kayıt yok",
			res:    fakeResolver{},
			domain: "yok.example.com", server: "203.0.113.10",
			want: Stop, reason: "kaydı yok",
		},
		{
			name: "A doğru, AAAA başka yeri gösteriyor ve sunucunun IPv6'sı biliniyor",
			res: fakeResolver{
				"ip4 app.example.com": {ips: []string{"203.0.113.10"}},
				"ip6 app.example.com": {ips: []string{"2001:db8::bad"}},
				"ip4 sunucu.example":  {ips: []string{"203.0.113.10"}},
				"ip6 sunucu.example":  {ips: []string{"2001:db8::10"}},
			},
			domain: "app.example.com", server: "sunucu.example",
			want: Stop, reason: "2001:db8::bad",
		},
		{
			// SSH hedefi IPv4 değişmezi: sunucunun IPv6'sı BİLİNMİYOR. Doğru bir
			// AAAA kaydı da olabilir; durdurmak yanlış olurdu.
			name: "A doğru, AAAA var ama sunucunun IPv6'sı bilinmiyor",
			res: fakeResolver{
				"ip4 app.example.com": {ips: []string{"203.0.113.10"}},
				"ip6 app.example.com": {ips: []string{"2001:db8::10"}},
			},
			domain: "app.example.com", server: "203.0.113.10",
			want: Warn, reason: "IPv6",
		},
		{
			name:   "sunucu Tailscale/CGNAT adresinde: genel adres bilinmiyor",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"198.51.100.7"}}},
			domain: "app.example.com", server: "100.101.102.103",
			want: Warn, reason: "genel adres",
		},
		{
			name:   "sunucu özel ağda",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"198.51.100.7"}}},
			domain: "app.example.com", server: "10.0.0.5",
			want: Warn, reason: "genel adres",
		},
		{
			name:   "yerel hedef: sunucu adresi yok",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"198.51.100.7"}}},
			domain: "app.example.com", server: "",
			want: Warn, reason: "genel adres",
		},
		{
			// Yerel hedefte bile "hiç kayıt yok" kesin bir bilgi.
			name:   "yerel hedef ve hiç kayıt yok",
			res:    fakeResolver{},
			domain: "yok.example.com", server: "",
			want: Stop, reason: "kaydı yok",
		},
		{
			name:   "DNS sorgusu bozuk: dağıtımı engellemez",
			res:    fakeResolver{"ip4 app.example.com": {err: errServfail}, "ip6 app.example.com": {err: errServfail}},
			domain: "app.example.com", server: "203.0.113.10",
			want: Warn, reason: "sorgu",
		},
		{
			name:   ".localhost denetlenmez",
			res:    fakeResolver{},
			domain: "hello.localhost", server: "203.0.113.10",
			want: Skipped,
		},
		{
			name:   "büyük harf ve sondaki nokta",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"203.0.113.10"}}},
			domain: "App.Example.COM.", server: "203.0.113.10",
			want: OK,
		},
		{
			name: "sunucu adla verilmiş, A kaydı ondan biri",
			res: fakeResolver{
				"ip4 app.example.com": {ips: []string{"203.0.113.11"}},
				"ip4 sunucu.example":  {ips: []string{"203.0.113.10", "203.0.113.11"}},
			},
			domain: "app.example.com", server: "sunucu.example",
			want: OK,
		},
		{
			// IPv4 eşlemeli IPv6 (::ffff:a.b.c.d) aynı adres sayılmalı.
			name:   "IPv4 eşlemeli IPv6 eşleşir",
			res:    fakeResolver{"ip4 app.example.com": {ips: []string{"::ffff:203.0.113.10"}}},
			domain: "app.example.com", server: "203.0.113.10",
			want: OK,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rep := Check(t.Context(), tc.res, tc.domain, tc.server)
			if rep.Verdict != tc.want {
				t.Fatalf("karar %v, beklenen %v; gerekçe: %q", rep.Verdict, tc.want, rep.Reasons)
			}
			if tc.reason != "" && !strings.Contains(strings.Join(rep.Reasons, "\n"), tc.reason) {
				t.Fatalf("gerekçede %q yok: %q", tc.reason, rep.Reasons)
			}
		})
	}
}

// Bir ailenin sorgusu bozuk, öbürü "bulunamadı" dönerse bu "kayıt yok"
// DEĞİL: bozuk sorgu kaydı gizliyor olabilir.
func TestALookupErrorIsNeverReadAsNoRecords(t *testing.T) {
	res := fakeResolver{"ip4 app.example.com": {err: errServfail}}
	rep := Check(t.Context(), res, "app.example.com", "203.0.113.10")
	if rep.Verdict != Warn {
		t.Fatalf("karar %v, beklenen Warn; gerekçe: %q", rep.Verdict, rep.Reasons)
	}
}

// Sözdizimi hatalı bir değer değil, gerçek bir ağ hatası: errors.Is ile
// sarılmış DNSError da tanınmalı.
func TestWrappedNotFoundIsRecognised(t *testing.T) {
	wrapped := errors.Join(errors.New("bağlam"), &net.DNSError{Err: "no such host", Name: "x", IsNotFound: true})
	res := fakeResolver{"ip4 yok.example.com": {err: wrapped}}
	rep := Check(t.Context(), res, "yok.example.com", "203.0.113.10")
	if rep.Verdict != Stop {
		t.Fatalf("karar %v, beklenen Stop; gerekçe: %q", rep.Verdict, rep.Reasons)
	}
}

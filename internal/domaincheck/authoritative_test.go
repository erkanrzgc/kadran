package domaincheck

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"sync"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
	"golang.org/x/net/dns/dnsmessage"
)

// İki ad sunucusu: ns1 → 192.0.2.1, ns2 → 192.0.2.2.
var twoNS = fakeResolver{
	"ip4 ns1.example-dns.net": {ips: []string{"192.0.2.1"}},
	"ip4 ns2.example-dns.net": {ips: []string{"192.0.2.2"}},
}

var nsNames = []string{"ns1.example-dns.net", "ns2.example-dns.net"}

func ask(t *testing.T, a1, a2 dnstest.Answer) Authoritative {
	t.Helper()
	ex := dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": a1, "192.0.2.2": a2})
	return AskAuthoritative(t.Context(), twoNS, ex, nsNames, "app.example.com")
}

func TestAskAuthoritative(t *testing.T) {
	ok := dnstest.Answer{AA: true, Addrs: []string{"203.0.113.10", "2001:db8::10"}, TTL: 600}

	got := ask(t, ok, dnstest.Answer{AA: true, Addrs: []string{"203.0.113.10", "2001:db8::10"}, TTL: 300})
	if !got.Answered || !got.Agree || len(got.Addrs) != 2 || got.TTL != 300 {
		t.Errorf("iki tutarlı yetkili cevap: %+v", got)
	}

	// AA bayraksız cevap (ISS araya girdi) yetkili sayılmaz.
	noAA := dnstest.Answer{Addrs: []string{"203.0.113.10"}, TTL: 600}
	if got := ask(t, noAA, noAA); got.Answered {
		t.Errorf("AA bayraksız cevap yetkili sayıldı: %+v", got)
	}

	// Biri AA, öbürü değil: yalnız AA olan sayılır, kendi içinde tutarlı.
	if got := ask(t, ok, noAA); !got.Answered || !got.Agree {
		t.Errorf("tek yetkili cevap: %+v", got)
	}

	// NS'ler farklı küme veriyor: Agree=false.
	other := dnstest.Answer{AA: true, Addrs: []string{"198.51.100.7"}, TTL: 600}
	if got := ask(t, ok, other); !got.Answered || got.Agree {
		t.Errorf("tutarsız NS'ler: %+v", got)
	}

	// Kimliği tutmayan cevap atılır.
	forged := ok
	forged.WrongID = true
	if got := ask(t, forged, forged); got.Answered {
		t.Errorf("kimliği tutmayan cevap kabul edildi: %+v", got)
	}

	// Cevap değil (QR yok) ya da soru bölümü tutmuyor: atılır.
	notResp := ok
	notResp.NotResponse = true
	if got := ask(t, notResp, notResp); got.Answered {
		t.Errorf("QR bayraksız mesaj cevap sayıldı: %+v", got)
	}
	wrongT := ok
	wrongT.WrongType = true
	if got := ask(t, wrongT, wrongT); got.Answered {
		t.Errorf("türü tutmayan cevap kabul edildi: %+v", got)
	}
	wrongQ := ok
	wrongQ.WrongQuestion = true
	if got := ask(t, wrongQ, wrongQ); got.Answered {
		t.Errorf("sorusu tutmayan cevap kabul edildi: %+v", got)
	}

	// Yalnız CNAME (hedef bölge dışında).
	cn := dnstest.Answer{AA: true, CNAME: "target.example.net", TTL: 300}
	if got := ask(t, cn, cn); !got.Answered || got.CNAME != "target.example.net" || len(got.Addrs) != 0 {
		t.Errorf("CNAME cevabı: %+v", got)
	}

	// Yetkili NXDOMAIN: cevaplandı, kayıt yok.
	nx := dnstest.Answer{AA: true, RCode: dnsmessage.RCodeNameError}
	if got := ask(t, nx, nx); !got.Answered || len(got.Addrs) != 0 {
		t.Errorf("yetkili NXDOMAIN: %+v", got)
	}

	// SERVFAIL yetkili cevap sayılmaz.
	sf := dnstest.Answer{AA: true, RCode: dnsmessage.RCodeServerFailure}
	if got := ask(t, sf, sf); got.Answered {
		t.Errorf("SERVFAIL yetkili sayıldı: %+v", got)
	}
}

// Adı çözülemeyen ad sunucusu atlanır, diğeri sayılır.
func TestAskAuthoritativeSkipsUnresolvableNameserver(t *testing.T) {
	ex := dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"203.0.113.10"}}})
	got := AskAuthoritative(t.Context(), twoNS, ex, []string{"ns1.example-dns.net", "yok.example-dns.net"}, "app.example.com")
	if !got.Answered || !got.Agree || len(got.Addrs) != 1 {
		t.Errorf("tek çözülen NS: %+v", got)
	}
}

// recordingExchange, hangi sunuculara sorgu gittiğini kaydeden sahte Exchanger.
type recordingExchange struct {
	mu      sync.Mutex
	servers map[string]bool
	inner   Exchanger
}

func newRecordingExchange(byServer map[string]dnstest.Answer) *recordingExchange {
	return &recordingExchange{servers: map[string]bool{}, inner: dnstest.Exchange(byServer)}
}

func (r *recordingExchange) exchange(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error) {
	r.mu.Lock()
	r.servers[server.Addr().String()] = true
	r.mu.Unlock()
	return r.inner(ctx, server, query)
}

// N1: herkese açık olmayan (loopback, özel, CGNAT) NS adresi sorgulanmaz;
// sonraki herkese açık adres kullanılır.
func TestAskAuthoritativeSkipsNonPublicNameserverAddresses(t *testing.T) {
	res := fakeResolver{
		"ip4 ns1.example-dns.net": {ips: []string{"127.0.0.1", "10.0.0.5", "100.64.0.9", "192.0.2.1"}},
		"ip4 ns2.example-dns.net": {ips: []string{"127.0.0.2"}},
	}
	rec := newRecordingExchange(map[string]dnstest.Answer{
		"127.0.0.1":  {AA: true, Addrs: []string{"203.0.113.10"}},
		"127.0.0.2":  {AA: true, Addrs: []string{"203.0.113.10"}},
		"10.0.0.5":   {AA: true, Addrs: []string{"203.0.113.10"}},
		"100.64.0.9": {AA: true, Addrs: []string{"203.0.113.10"}},
		"192.0.2.1":  {AA: true, Addrs: []string{"203.0.113.10"}},
	})

	got := AskAuthoritative(t.Context(), res, rec.exchange, nsNames, "app.example.com")

	if len(rec.servers) != 1 || !rec.servers["192.0.2.1"] {
		t.Errorf("sorgulanan sunucular %v; yalnız 192.0.2.1 bekleniyordu", rec.servers)
	}
	if !got.Answered {
		t.Errorf("herkese açık NS cevap vermeliydi: %+v", got)
	}
}

// listenUDPAndTCP, aynı yerel portta UDP ve TCP açar. Port UDP'de seçilir; TCP
// o portu başka bir süreç almışsa baştan denenir (çakışma testi bozmasın).
func listenUDPAndTCP(t *testing.T) (net.PacketConn, net.Listener) {
	t.Helper()
	for range 20 {
		udp, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		tcp, err := net.Listen("tcp", udp.LocalAddr().String())
		if err == nil {
			return udp, tcp
		}
		_ = udp.Close()
	}
	t.Fatal("UDP ve TCP için aynı yerel port bulunamadı")
	return nil, nil
}

// Gerçek UDP/TCP: kesik UDP cevabında TCP'ye geçilmeli.
func TestDefaultExchangeFallsBackToTCPWhenTruncated(t *testing.T) {
	udp, tcpLn := listenUDPAndTCP(t)
	defer func() { _ = udp.Close() }()
	defer func() { _ = tcpLn.Close() }()

	full := dnstest.Answer{AA: true, Addrs: []string{"203.0.113.10"}, TTL: 60}
	udpSeen := make(chan struct{})
	go func() { // UDP: kesik cevap
		buf := make([]byte, 512)
		n, addr, err := udp.ReadFrom(buf)
		if err != nil {
			return
		}
		close(udpSeen)
		resp, _ := dnstest.Reply(buf[:n], dnstest.Answer{AA: true, Truncated: true})
		_, _ = udp.WriteTo(resp, addr)
	}()
	go func() { // TCP: tam cevap
		conn, err := tcpLn.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var n [2]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return
		}
		q := make([]byte, binary.BigEndian.Uint16(n[:]))
		if _, err := io.ReadFull(conn, q); err != nil {
			return
		}
		resp, _ := dnstest.Reply(q, full)
		out := binary.BigEndian.AppendUint16(nil, uint16(len(resp)))
		_, _ = conn.Write(append(out, resp...))
	}()

	query, id, q, err := newQuery("app.example.com", dnsmessage.TypeA)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	resp, err := defaultExchange(ctx, netip.MustParseAddrPort(udp.LocalAddr().String()), query)
	if err != nil {
		t.Fatalf("defaultExchange: %v", err)
	}
	select {
	case <-udpSeen:
	case <-time.After(time.Second):
		t.Fatal("sorgu önce UDP ile gitmedi")
	}
	a, ok := parseAnswer(resp, id, q)
	if !ok || len(a.addrs) != 1 || a.addrs[0].String() != "203.0.113.10" {
		t.Fatalf("TCP'ye geçilmedi ya da cevap okunamadı: %+v (%v)", a, ok)
	}
}

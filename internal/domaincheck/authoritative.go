package domaincheck

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// QueryTimeout, tek bir yetkili sorgunun süre sınırı.
const QueryTimeout = 3 * time.Second

const dnsPort = 53

// Exchanger, bir DNS sorgusunu sunucuya gönderip ham cevabı döndürür.
// Testler sahtesini veriyor (dnstest.Exchange).
type Exchanger func(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error)

// Authoritative, bölgenin ad sunucularının cevabı (K-151).
type Authoritative struct {
	// Answered: en az bir ad sunucusu AA bayraklı, geçerli bir cevap verdi.
	// Bayraksız cevap 53. portu araya giren bir ISS önbelleğinden gelmiş
	// olabilir; sayılmaz.
	Answered bool
	// Agree: cevap veren bütün ad sunucuları aynı kümeyi verdi.
	Agree bool
	Addrs []netip.Addr
	// CNAME: uzlaşılan cevap bir CNAME taşıyorsa hedefi (bölge içi hedefte adresler de gelir).
	CNAME string
	// TTL: cevaplardaki en küçük TTL (saniye); kayıt yoksa 0.
	TTL uint32
}

type nsAnswer struct {
	addrs []netip.Addr
	cname string
	ttl   uint32
}

// AskAuthoritative, bölgenin HER ad sunucusuna name'in A ve AAAA kaydını
// sorar. Sorgular paralel; her biri QueryTimeout ile sınırlı ve hepsi
// ctx'in süresine tabi. Hiçbiri cevap vermezse Answered=false döner ve
// çağıran yerel çözümleyiciye düşer.
func AskAuthoritative(ctx context.Context, r Resolver, ex Exchanger, nameservers []string, name string) Authoritative {
	if ex == nil {
		ex = defaultExchange
	}
	answers := make([]*nsAnswer, len(nameservers))
	var wg sync.WaitGroup
	for i, ns := range nameservers {
		wg.Go(func() {
			if a, ok := askOne(ctx, r, ex, ns, name); ok {
				answers[i] = &a
			}
		})
	}
	wg.Wait()

	var got []nsAnswer
	for _, a := range answers {
		if a != nil {
			got = append(got, *a)
		}
	}
	if len(got) == 0 {
		return Authoritative{}
	}
	out := Authoritative{Answered: true, Agree: true, Addrs: got[0].addrs, CNAME: got[0].cname, TTL: got[0].ttl}
	for _, a := range got[1:] {
		if !sameAnswer(a, got[0]) {
			out.Agree = false
		}
		out.TTL = minTTL(out.TTL, a.ttl)
	}
	return out
}

// askOne, tek bir ad sunucusuna A ve AAAA sorar; ikisi de geçerli
// cevaplanmalı.
func askOne(ctx context.Context, r Resolver, ex Exchanger, ns, name string) (nsAnswer, bool) {
	server, ok := nsAddress(ctx, r, ns)
	if !ok {
		return nsAnswer{}, false
	}
	var all nsAnswer
	for _, qt := range []dnsmessage.Type{dnsmessage.TypeA, dnsmessage.TypeAAAA} {
		a, ok := queryOne(ctx, ex, server, name, qt)
		if !ok {
			return nsAnswer{}, false
		}
		all.addrs = append(all.addrs, a.addrs...)
		if a.cname != "" {
			all.cname = a.cname
		}
		all.ttl = minTTL(all.ttl, a.ttl)
	}
	return all, true
}

// nsAddress, ad sunucusunun adresini bulur. IPv4 önce: yalnız IPv4'lü bir
// ağda IPv6 adresinde beklenmesin. Herkese açık olmayan adres (loopback, özel,
// CGNAT) atlanır: düşmanca bir bölge NS adlarını kullanıcının LAN'ına ya da
// kendi makinesine çözdürüp oraya sorgu yağdıramasın.
func nsAddress(ctx context.Context, r Resolver, ns string) (netip.AddrPort, bool) {
	for _, network := range []string{"ip4", "ip6"} {
		addrs, _, _ := lookup(ctx, r, network, ns)
		for _, a := range addrs {
			if isPublic(a) {
				return netip.AddrPortFrom(a, dnsPort), true
			}
		}
	}
	return netip.AddrPort{}, false
}

func queryOne(ctx context.Context, ex Exchanger, server netip.AddrPort, name string, qt dnsmessage.Type) (nsAnswer, bool) {
	query, id, q, err := newQuery(name, qt)
	if err != nil {
		return nsAnswer{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, QueryTimeout)
	defer cancel()
	resp, err := ex(ctx, server, query)
	if err != nil {
		return nsAnswer{}, false
	}
	return parseAnswer(resp, id, q)
}

// newQuery, rastgele kimlikli, özyinelemesiz (RD=0) tek soruluk bir sorgu
// kurar: yetkili sunucuya kendi bölgesi soruluyor.
func newQuery(name string, qt dnsmessage.Type) ([]byte, uint16, dnsmessage.Question, error) {
	qname, err := dnsmessage.NewName(name + ".")
	if err != nil {
		return nil, 0, dnsmessage.Question{}, err
	}
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, 0, dnsmessage.Question{}, err
	}
	id := binary.BigEndian.Uint16(b[:])
	q := dnsmessage.Question{Name: qname, Type: qt, Class: dnsmessage.ClassINET}
	msg := dnsmessage.Message{Header: dnsmessage.Header{ID: id}, Questions: []dnsmessage.Question{q}}
	packed, err := msg.Pack()
	return packed, id, q, err
}

// parseAnswer, cevabı doğrular ve okur. Atılan cevaplar: yanıt değil,
// kimlik ya da soru tutmuyor, AA yok, RCODE başarı/NXDOMAIN dışı.
func parseAnswer(resp []byte, id uint16, q dnsmessage.Question) (nsAnswer, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(resp)
	if err != nil || !h.Response || h.ID != id || !h.Authoritative {
		return nsAnswer{}, false
	}
	if h.RCode != dnsmessage.RCodeSuccess && h.RCode != dnsmessage.RCodeNameError {
		return nsAnswer{}, false
	}
	got, err := p.Question()
	if err != nil || got.Type != q.Type || !strings.EqualFold(got.Name.String(), q.Name.String()) {
		return nsAnswer{}, false
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nsAnswer{}, false
	}
	var a nsAnswer
	for {
		rh, err := p.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return a, true
		}
		if err != nil {
			return nsAnswer{}, false
		}
		a.ttl = minTTL(a.ttl, rh.TTL)
		switch rh.Type {
		case dnsmessage.TypeA:
			r, err := p.AResource()
			if err != nil {
				return nsAnswer{}, false
			}
			a.addrs = append(a.addrs, netip.AddrFrom4(r.A))
		case dnsmessage.TypeAAAA:
			r, err := p.AAAAResource()
			if err != nil {
				return nsAnswer{}, false
			}
			a.addrs = append(a.addrs, netip.AddrFrom16(r.AAAA).Unmap())
		case dnsmessage.TypeCNAME:
			r, err := p.CNAMEResource()
			if err != nil {
				return nsAnswer{}, false
			}
			a.cname = CleanName(r.CNAME.String())
		default:
			if err := p.SkipAnswer(); err != nil {
				return nsAnswer{}, false
			}
		}
	}
}

// minTTL, sıfırı "henüz yok" sayan en küçük TTL.
func minTTL(a, b uint32) uint32 {
	switch {
	case a == 0:
		return b
	case b == 0:
		return a
	default:
		return min(a, b)
	}
}

func sameAnswer(a, b nsAnswer) bool {
	return a.cname == b.cname && sameSet(a.addrs, b.addrs)
}

// sameSet, iki adres kümesinin (sıra ve tekrar önemsiz) eşit olduğunu söyler.
func sameSet(a, b []netip.Addr) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.SortFunc(x, netip.Addr.Compare)
	slices.SortFunc(y, netip.Addr.Compare)
	return slices.Equal(slices.Compact(x), slices.Compact(y))
}

// defaultExchange, gerçek ağ: UDP; TC bayraklı cevapta TCP.
func defaultExchange(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error) {
	resp, err := exchangeOver(ctx, "udp", server, query)
	if err != nil {
		return nil, err
	}
	var p dnsmessage.Parser
	if h, err := p.Start(resp); err == nil && h.Truncated {
		return exchangeOver(ctx, "tcp", server, query)
	}
	return resp, nil
}

// maxDNSMessage, UDP ve TCP'de okunacak en büyük DNS mesajı.
const maxDNSMessage = 65535

func exchangeOver(ctx context.Context, network string, server netip.AddrPort, query []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, server.String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	if network == "tcp" {
		msg := binary.BigEndian.AppendUint16(nil, uint16(len(query)))
		if _, err := conn.Write(append(msg, query...)); err != nil {
			return nil, err
		}
		var n [2]byte
		if _, err := io.ReadFull(conn, n[:]); err != nil {
			return nil, err
		}
		buf := make([]byte, binary.BigEndian.Uint16(n[:]))
		if _, err := io.ReadFull(conn, buf); err != nil {
			return nil, err
		}
		return buf, nil
	}
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, maxDNSMessage)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

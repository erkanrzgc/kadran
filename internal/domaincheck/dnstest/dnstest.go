// Package dnstest, domaincheck ve CLI testleri için sahte bir yetkili DNS
// sunucusu (K-151). Ağ yok: sorgu baytlarını okuyup cevap baytlarını kurar.
package dnstest

import (
	"context"
	"net/netip"

	"golang.org/x/net/dns/dnsmessage"
)

// Answer, bir ad sunucusunun vereceği cevap.
type Answer struct {
	// AA: yetkili cevap bayrağı. false, 53. portu araya giren bir ISS
	// önbelleğinin cevabını taklit eder.
	AA    bool
	RCode dnsmessage.RCode
	// Addrs: A ve AAAA birlikte; sorulan aileye göre süzülür.
	Addrs []string
	CNAME string
	TTL   uint32
	// WrongID: cevabın kimliği sorguyla eşleşmez (sahte cevap).
	WrongID bool
	// Truncated: TC bayrağı; istemci TCP'ye geçmeli.
	Truncated bool
	// NotResponse: QR bayrağı yok (cevap değil, sorgu yankısı).
	NotResponse bool
	// WrongQuestion: cevabın soru bölümü sorulandan farklı (sahte cevap).
	WrongQuestion bool
	// Block: bağlam bitene kadar cevap vermez (süre bütçesi testi).
	Block bool
}

// Reply, query'ye a'yı cevap olarak kurar.
func Reply(query []byte, a Answer) ([]byte, error) {
	var p dnsmessage.Parser
	h, err := p.Start(query)
	if err != nil {
		return nil, err
	}
	q, err := p.Question()
	if err != nil {
		return nil, err
	}
	if a.WrongQuestion {
		q.Name = dnsmessage.MustNewName("other.example.org.")
	}
	id := h.ID
	if a.WrongID {
		id++
	}
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{
		ID: id, Response: !a.NotResponse, Authoritative: a.AA, RCode: a.RCode, Truncated: a.Truncated,
	})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(q); err != nil {
		return nil, err
	}
	if err := b.StartAnswers(); err != nil {
		return nil, err
	}
	rh := dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: a.TTL}
	if a.CNAME != "" {
		target, err := dnsmessage.NewName(a.CNAME + ".")
		if err != nil {
			return nil, err
		}
		if err := b.CNAMEResource(rh, dnsmessage.CNAMEResource{CNAME: target}); err != nil {
			return nil, err
		}
	}
	for _, s := range a.Addrs {
		addr := netip.MustParseAddr(s)
		switch {
		case addr.Is4() && q.Type == dnsmessage.TypeA:
			if err := b.AResource(rh, dnsmessage.AResource{A: addr.As4()}); err != nil {
				return nil, err
			}
		case addr.Is6() && q.Type == dnsmessage.TypeAAAA:
			if err := b.AAAAResource(rh, dnsmessage.AAAAResource{AAAA: addr.As16()}); err != nil {
				return nil, err
			}
		}
	}
	return b.Finish()
}

// Exchange, sunucu IP'sine (port hariç) göre cevap veren sahte bir
// domaincheck.Exchanger. Tanımsız sunucu bağlantı hatası verir.
func Exchange(byServer map[string]Answer) func(context.Context, netip.AddrPort, []byte) ([]byte, error) {
	return func(ctx context.Context, server netip.AddrPort, query []byte) ([]byte, error) {
		a, ok := byServer[server.Addr().String()]
		if !ok {
			return nil, context.DeadlineExceeded
		}
		if a.Block {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return Reply(query, a)
	}
}

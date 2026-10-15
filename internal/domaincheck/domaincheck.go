// Package domaincheck, bir alan adının DNS kaydının Kadran sunucusunu
// gösterip göstermediğini, uygulamaya yazılmadan ÖNCE denetler (K-128).
//
// Neden: DNS yanlışsa ya da 80/443 kapalıysa `app create/update -domain`
// başarılı dönüyordu, sertifika alınamıyordu ve bunu söyleyen bir şey
// yoktu. Caddy başarısız denemeden sonra üstel bekliyor (denemeler arası 1
// güne kadar); kullanıcı DNS'i düzeltse de HTTPS geç geliyordu.
//
// Denetimi CLI yapıyor: kadrand ağa çıkamıyor (`IPAddressDeny=any`) ve
// sunucu kendi genel adresini bilemeyebiliyor (GCP'de arayüzde özel adres
// var, K-121). CLI ise sunucunun adresini SSH hedefinden biliyor.
//
// Karar kuralları:
//   - Bir aile için yalnızca sunucunun O ailedeki adresi biliniyorsa
//     durulur: IPv4 değişmeziyle bağlanılan bir sunucunun IPv6'sı
//     bilinmez ve doğru bir AAAA kaydı da reddedilirdi.
//   - "Kayıt yok" yalnızca iki ailenin İKİSİ de "bulunamadı" döndüğünde.
//     Bozuk bir sorgu (zaman aşımı, SERVFAIL) kaydı gizliyor olabilir; o
//     yalnızca uyarır. Dalgalı DNS bir dağıtımı asla engellememeli.
//   - `.localhost` denetlenmez: Caddy onlar için iç sertifikasını kullanır.
//   - K-151: bölgenin ad sunucuları biliniyorsa karar ONLARIN cevabına
//     göre verilir (yerel önbellek eski olabilir); sunucunun adresiyle
//     başka adres birlikte dönerse durulur; Cloudflare aralığı ayrı
//     anlatılır. Hepsi DefaultBudget içinde; aşılırsa yukarıdaki yerel
//     kurallar geçerli.
package domaincheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"
)

// Verdict, denetimin sonucu. Değerler artan önem sırasında.
type Verdict int

const (
	// OK: kayıtlar sunucuyu gösteriyor.
	OK Verdict = iota
	// Skipped: denetlenmedi (ör. `.localhost`).
	Skipped
	// Warn: belirsiz; devam edilir.
	Warn
	// Stop: açıkça yanlış; durulur.
	Stop
)

func (v Verdict) String() string {
	switch v {
	case OK:
		return "ok"
	case Skipped:
		return "skipped"
	case Warn:
		return "warning"
	case Stop:
		return "stop"
	}
	return fmt.Sprintf("Verdict(%d)", int(v))
}

// LookupTimeout, tek bir DNS sorgusunun süre sınırı; komutun kendi
// süresinden ayrı.
const LookupTimeout = 5 * time.Second

// Resolver, *net.Resolver'ın kullanılan yöntemi; testler sahtesini veriyor.
type Resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// Fix, kullanıcının DNS'te yapması gerekenler (K-151).
type Fix struct {
	Add    []Record
	Remove []Record
}

// Report, denetimin bulguları.
type Report struct {
	Domain  string
	Verdict Verdict
	// Server, sunucunun bilinen GENEL adresleri.
	Server  []netip.Addr
	A, AAAA []netip.Addr
	Reasons []string

	// ── K-151: DNS'in nerede olduğu ve ne yapılacağı ─────────────────
	Zone        string
	NameServers []string
	// ZoneChecked: NS sorgusu yapıldı (Checker.NS verildi). Zone boşsa
	// bölge bulunamadı: domain kayıtlı ya da devredilmiş olmayabilir.
	ZoneChecked bool
	Provider    Provider
	HasProvider bool
	// RecordName, kaydın bölge içindeki adı ("@" ya da "kadran").
	RecordName string
	Auth       Authoritative
	// CNAME, yetkili cevap yalnız CNAME döndüyse hedefi.
	CNAME string
	// Proxied: Cloudflare aralığı ve NS Cloudflare'de (turuncu bulut).
	Proxied bool
	// OnCloudflare: Cloudflare aralığı, NS başka firmada (ör. park sayfası).
	OnCloudflare bool
	Fix          Fix
}

func (r *Report) raise(v Verdict, format string, args ...any) {
	if v > r.Verdict {
		r.Verdict = v
	}
	r.Reasons = append(r.Reasons, fmt.Sprintf(format, args...))
}

// cgnat, 100.64.0.0/10 (RFC 6598): taşıyıcı NAT'ı ve Tailscale.
// netip.Addr.IsPrivate bu aralığı kapsamıyor.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// isPublic, adresin internetten erişilebilir bir sunucu adresi olup
// olamayacağını söyler.
func isPublic(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsGlobalUnicast() && !a.IsPrivate() && !cgnat.Contains(a)
}

// lookup, tek bir aileyi sorar. notFound: kesin "bu ailede kayıt yok".
func lookup(ctx context.Context, r Resolver, network, host string) (addrs []netip.Addr, notFound bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	ips, err := r.LookupIP(ctx, network, host)
	if err != nil {
		var de *net.DNSError
		if errors.As(err, &de) && de.IsNotFound {
			return nil, true, nil
		}
		return nil, false, err
	}
	for _, ip := range ips {
		if a, ok := netip.AddrFromSlice(ip); ok {
			addrs = append(addrs, a.Unmap())
		}
	}
	if len(addrs) == 0 {
		return nil, true, nil
	}
	return addrs, false, nil
}

// serverAddrs, SSH hedefinin genel adreslerini bulur. Boş hedef (yerel
// soket) ve çözülemeyen ad boş liste döndürür.
func serverAddrs(ctx context.Context, r Resolver, host string) []netip.Addr {
	if host == "" {
		return nil
	}
	var all []netip.Addr
	if a, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		all = []netip.Addr{a.Unmap()}
	} else {
		for _, network := range []string{"ip4", "ip6"} {
			addrs, _, _ := lookup(ctx, r, network, host)
			all = append(all, addrs...)
		}
	}
	var public []netip.Addr
	for _, a := range all {
		if isPublic(a) {
			public = append(public, a)
		}
	}
	return public
}

func family(addrs []netip.Addr, v4 bool) []netip.Addr {
	var out []netip.Addr
	for _, a := range addrs {
		if a.Is4() == v4 {
			out = append(out, a)
		}
	}
	return out
}

func overlaps(a, b []netip.Addr) bool {
	for _, x := range a {
		if slices.Contains(b, x) {
			return true
		}
	}
	return false
}

func join(addrs []netip.Addr) string {
	s := make([]string, len(addrs))
	for i, a := range addrs {
		s[i] = a.String()
	}
	return strings.Join(s, ", ")
}

// DefaultBudget, bölge yürüyüşü ve yetkili sorguların toplam süresi.
// Aşılırsa yalnız yerel çözümleyiciyle karar verilir; komut takılmaz.
const DefaultBudget = 10 * time.Second

// Checker, alan adı denetimini yapar. NS nil ise bölge, firma ve yetkili
// sorgu yapılmaz: K-128'in yalnız-yerel davranışı.
type Checker struct {
	Resolver Resolver
	NS       NSResolver
	// Exchange nil ise gerçek ağ (UDP/TCP 53).
	Exchange Exchanger
	// Budget sıfırsa DefaultBudget.
	Budget time.Duration
}

// Check, K-128 uyumlu kısayol: yalnız yerel çözümleyici.
func Check(ctx context.Context, r Resolver, domain, serverHost string) Report {
	return Checker{Resolver: r}.Check(ctx, domain, serverHost)
}

// Check, alan adının A/AAAA kayıtlarını sunucunun adresleriyle
// karşılaştırır. serverHost: SSH hedefinin ad ya da adresi; yerel hedefte
// boş.
func (c Checker) Check(ctx context.Context, domain, serverHost string) Report {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	rep := Report{Domain: domain, Verdict: OK, RecordName: guessRecordName(domain)}

	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		rep.raise(Skipped, "%s is a .localhost name: it is not in DNS, Caddy uses its internal certificate", domain)
		return rep
	}

	a, aNotFound, aErr := lookup(ctx, c.Resolver, "ip4", domain)
	aaaa, aaaaNotFound, aaaaErr := lookup(ctx, c.Resolver, "ip6", domain)
	rep.A, rep.AAAA = a, aaaa
	rep.Server = serverAddrs(ctx, c.Resolver, serverHost)
	c.locate(ctx, &rep)

	if rep.Auth.Answered && rep.Auth.Agree {
		c.judgeAuthoritative(ctx, &rep, serverHost)
	} else {
		for _, e := range []error{aErr, aaaaErr} {
			if e != nil {
				rep.raise(Warn, "DNS lookup did not complete (%v); records could not be checked", e)
			}
		}
		judge(&rep, a, aaaa, aNotFound && aaaaNotFound, serverHost)
		if rep.Auth.Answered && rep.Verdict < Stop {
			rep.raise(Warn, "the DNS host's nameservers do not agree yet (%s): the change is still spreading inside the DNS host",
				strings.Join(rep.NameServers, ", "))
		}
	}
	rep.Fix.Add = fixRecords(rep)
	return rep
}

// locate, bölgeyi, firmayı ve yetkili cevabı bulur; tamamı Budget içinde.
func (c Checker) locate(ctx context.Context, rep *Report) {
	if c.NS == nil {
		return
	}
	rep.ZoneChecked = true
	budget := c.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	zone, ok := FindZone(ctx, c.NS, rep.Domain)
	if !ok {
		return
	}
	rep.Zone, rep.NameServers = zone.Name, zone.NameServers
	rep.RecordName = RecordName(rep.Domain, zone.Name)
	rep.Provider, rep.HasProvider = IdentifyProvider(zone.NameServers)
	rep.Auth = AskAuthoritative(ctx, c.Resolver, c.Exchange, zone.NameServers, rep.Domain)
}

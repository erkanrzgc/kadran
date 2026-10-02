// Package domaincheck, bir alan adının DNS kaydının Panely sunucusunu
// gösterip göstermediğini, uygulamaya yazılmadan ÖNCE denetler (K-128).
//
// Neden: DNS yanlışsa ya da 80/443 kapalıysa `app create/update -domain`
// başarılı dönüyordu, sertifika alınamıyordu ve bunu söyleyen bir şey
// yoktu. Caddy başarısız denemeden sonra üstel bekliyor (denemeler arası 1
// güne kadar); kullanıcı DNS'i düzeltse de HTTPS geç geliyordu.
//
// Denetimi CLI yapıyor: panelyd ağa çıkamıyor (`IPAddressDeny=any`) ve
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
		return "atlandı"
	case Warn:
		return "uyarı"
	case Stop:
		return "dur"
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

// Report, denetimin bulguları.
type Report struct {
	Domain  string
	Verdict Verdict
	// Server, sunucunun bilinen GENEL adresleri.
	Server  []netip.Addr
	A, AAAA []netip.Addr
	Reasons []string
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

// Check, alan adının A/AAAA kayıtlarını sunucunun adresleriyle
// karşılaştırır. serverHost: SSH hedefinin ad ya da adresi; yerel hedefte
// boş.
func Check(ctx context.Context, r Resolver, domain, serverHost string) Report {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	rep := Report{Domain: domain, Verdict: OK}

	if domain == "localhost" || strings.HasSuffix(domain, ".localhost") {
		rep.raise(Skipped, "%s bir .localhost adı: DNS'te yok, Caddy iç sertifikasını kullanır", domain)
		return rep
	}

	a, aNotFound, aErr := lookup(ctx, r, "ip4", domain)
	aaaa, aaaaNotFound, aaaaErr := lookup(ctx, r, "ip6", domain)
	rep.A, rep.AAAA = a, aaaa
	for _, e := range []error{aErr, aaaaErr} {
		if e != nil {
			rep.raise(Warn, "DNS sorgusu tamamlanamadı (%v); kayıtlar denetlenemedi", e)
		}
	}
	if aNotFound && aaaaNotFound {
		rep.raise(Stop, "%s için A ya da AAAA kaydı yok", domain)
		return rep
	}

	rep.Server = serverAddrs(ctx, r, serverHost)
	if len(rep.Server) == 0 {
		if serverHost == "" {
			rep.raise(Warn, "sunucunun genel adresi bilinmiyor (yerel hedef); kayıtlar: %s", join(append(a, aaaa...)))
		} else {
			rep.raise(Warn, "sunucunun genel adresi bilinmiyor (%s özel ağda ya da çözülemedi); kayıtlar: %s",
				serverHost, join(append(a, aaaa...)))
		}
		return rep
	}

	checkFamily(&rep, "A", "IPv4", a, family(rep.Server, true))
	checkFamily(&rep, "AAAA", "IPv6", aaaa, family(rep.Server, false))
	return rep
}

// checkFamily, bir ailenin kayıtlarını sunucunun o ailedeki adresleriyle
// karşılaştırır. Sunucunun o ailedeki adresi bilinmiyorsa durmaz, uyarır.
func checkFamily(rep *Report, record, fam string, records, server []netip.Addr) {
	switch {
	case len(records) == 0:
		return
	case len(server) == 0:
		rep.raise(Warn, "%s kaydı var (%s) ama sunucunun %s adresi bilinmiyor; "+
			"başka bir yeri gösteriyorsa sertifika alınamaz", record, join(records), fam)
	case overlaps(records, server):
		return
	default:
		rep.raise(Stop, "%s kaydı %s gösteriyor, sunucu %s", record, join(records), join(server))
	}
}

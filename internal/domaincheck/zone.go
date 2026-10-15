package domaincheck

import (
	"context"
	"net"
	"slices"
	"strings"

	"golang.org/x/net/publicsuffix"
)

// NSResolver, *net.Resolver'ın NS sorgusu; testler sahtesini veriyor.
type NSResolver interface {
	LookupNS(ctx context.Context, name string) ([]*net.NS, error)
}

// Zone, adın DNS bölgesi ve ad sunucuları (temizlenmiş, sıralı).
type Zone struct {
	Name        string
	NameServers []string
}

// FindZone, adın kendisinden kayıt edilebilir alan adına (eTLD+1) kadar
// çıkar ve NS kaydı olan ilk adı döndürür. Genel son eke (co.uk, com.tr)
// ASLA tırmanmaz: orası kullanıcının bölgesi değil, kayıt otoritesi.
func FindZone(ctx context.Context, r NSResolver, domain string) (Zone, bool) {
	top, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return Zone{}, false
	}
	name := domain
	for {
		if ns := lookupNS(ctx, r, name); len(ns) > 0 {
			return Zone{Name: name, NameServers: ns}, true
		}
		if name == top {
			return Zone{}, false
		}
		_, rest, ok := strings.Cut(name, ".")
		if !ok {
			return Zone{}, false
		}
		name = rest
	}
}

func lookupNS(ctx context.Context, r NSResolver, name string) []string {
	ctx, cancel := context.WithTimeout(ctx, LookupTimeout)
	defer cancel()
	recs, err := r.LookupNS(ctx, name)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(recs))
	for _, ns := range recs {
		if n := CleanName(ns.Host); n != "" && !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}

// RecordName, kaydın bölge içindeki adı: bölgenin kendisi "@".
func RecordName(domain, zone string) string {
	if domain == zone {
		return "@"
	}
	return strings.TrimSuffix(domain, "."+zone)
}

// guessRecordName, bölge bilinmeden kaydın adını tahmin eder: kayıt
// edilebilir alan adının (eTLD+1) kendisi "@". NS sorgusu kapalıyken (K-128
// yolu) ya da başarısızken de tarif bir ad verebilsin diye.
func guessRecordName(domain string) string {
	top, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return domain
	}
	return RecordName(domain, top)
}

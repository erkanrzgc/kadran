// Package appnames, bir uygulamanın www eşini türetir ve uygulamalar arası
// ad çakışmasını bulur. Yalnız proxydrv.ValidDomain'i kullanır; proxydrv bu
// paketi içe aktarmaz.
package appnames

import (
	"slices"

	"golang.org/x/net/publicsuffix"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

// maxNameLen, bir DNS adının en fazla uzunluğudur.
const maxNameLen = 253

// Partner, domainin www eşini döndürür: eTLD+1 ise "www."+domain, "www."+eTLD1
// ise eTLD1. Alt ad, hata ya da geçersiz sonuç için "" döner.
func Partner(domain string) string {
	base, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return ""
	}
	var partner string
	switch domain {
	case base:
		partner = "www." + domain
	case "www." + base:
		partner = base
	default:
		return ""
	}
	if len(partner) > maxNameLen || !proxydrv.ValidDomain(partner) {
		return ""
	}
	return partner
}

// Names, uygulamanın domaini dışındaki tüm adlarını döndürür: www açıksa ve eş
// varsa önce eş, sonra sıralı ve tekrarsız açık adlar. Domain boşsa nil.
func Names(domain string, www bool, redirects []string) []string {
	if domain == "" {
		return nil
	}
	var names []string
	if www {
		if p := Partner(domain); p != "" {
			names = append(names, p)
		}
	}
	for _, r := range slices.Sorted(slices.Values(redirects)) {
		if r == domain || slices.Contains(names, r) {
			continue
		}
		names = append(names, r)
	}
	return names
}

// Owner, çakışma denetiminde bir uygulamanın ad kaydıdır.
type Owner struct {
	AppID     string
	Domain    string
	WWW       bool
	Redirects []string
}

// Conflict, c'nin domaini ya da adlarından biri başka bir sahibin domaini ya da
// adlarıyla çakışıyorsa ilk çakışan adı ve sahibini döndürür. Önce c.Domain,
// sonra c'nin adları; sahipler verilen sırayla taranır.
func Conflict(c Owner, others []Owner) (name, ownerID string, found bool) {
	mine := append([]string{c.Domain}, Names(c.Domain, c.WWW, c.Redirects)...)
	for _, n := range mine {
		if n == "" { // domainsiz kayıt bir ad değildir
			continue
		}
		for _, o := range others {
			if o.AppID == c.AppID {
				continue
			}
			if o.Domain == n || slices.Contains(Names(o.Domain, o.WWW, o.Redirects), n) {
				return n, o.AppID, true
			}
		}
	}
	return "", "", false
}

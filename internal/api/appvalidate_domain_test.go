package api

import (
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

// API'nin kabul ettiği her alan adı vekilin FixedConfig'inden de
// geçmeli; aksi hâlde tek uygulama bütün rota kümesini düşürür.
func TestValidateDomainAgreesWithProxyDriver(t *testing.T) {
	for _, d := range []string{
		"blog.example.com", "a.b", "203.0.113.5", "10.0.0.1",
		strings.Repeat("a", 63) + ".example.com", strings.Repeat("a", 64) + ".example.com",
		"-x.example.com", "x-.example.com", "Blog.Example.com", "x.com:443", "x.com/y", "a..b",
	} {
		apiOK := validateDomain(d) == nil
		proxyOK := proxydrv.ValidDomain(d)
		if apiOK != proxyOK {
			t.Errorf("%q: api=%v proxy=%v", d, apiOK, proxyOK)
		}
		if apiOK {
			set := proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{
				Domain:    d,
				Upstreams: []proxydrv.ProxyUpstream{{IP: "172.18.0.5", Port: 8080}},
			}}}
			if _, err := proxydrv.FixedConfig(set); err != nil {
				t.Errorf("%q API'den geçti ama vekil reddetti: %v", d, err)
			}
		}
	}
}

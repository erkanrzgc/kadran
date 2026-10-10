package proxydrv

import (
	"fmt"
	"net/netip"
	"reflect"
	"strings"
)

// RouteSet is the complete capability exposed to the control plane. It has
// no handler, response body, file path, admin address or listener fields.
type RouteSet struct {
	Routes []ProxyRoute `json:"routes"`
}
type ProxyRoute struct {
	Domain    string          `json:"domain"`
	Upstreams []ProxyUpstream `json:"upstreams"`
}
type ProxyUpstream struct {
	IP   string `json:"ip"`
	Port uint32 `json:"port"`
}

const MaxRouteRequest = 1 << 20
const MaxRoutes = 1024
const MaxRouteUpstreams = 64

// FixedConfig constructs the only configuration the broker can load. Docker
// must use the same 172.16/12 pool permitted by kadrand's network sandbox.
func FixedConfig(set RouteSet) (*Config, error) {
	if set.Routes == nil {
		return nil, fmt.Errorf("proxy routes array is required")
	}
	if len(set.Routes) > MaxRoutes {
		return nil, fmt.Errorf("too many proxy routes")
	}
	routes := make([]AppRoute, 0, len(set.Routes))
	for i, r := range set.Routes {
		if !validDomain(r.Domain) {
			return nil, fmt.Errorf("invalid proxy domain %q", r.Domain)
		}
		if len(r.Upstreams) == 0 || len(r.Upstreams) > MaxRouteUpstreams {
			return nil, fmt.Errorf("invalid upstream count for %q", r.Domain)
		}
		upstreams := make([]Upstream, 0, len(r.Upstreams))
		seen := map[string]bool{}
		for _, u := range r.Upstreams {
			addr, err := netip.ParseAddr(u.IP)
			if err != nil || addr.Zone() != "" || !netip.MustParsePrefix("172.16.0.0/12").Contains(addr) {
				return nil, fmt.Errorf("upstream must be a Docker IPv4 address in 172.16.0.0/12")
			}
			dial, err := NewUpstream(u.IP, u.Port)
			if err != nil {
				return nil, err
			}
			if seen[dial.Dial] {
				return nil, fmt.Errorf("duplicate upstream for %q", r.Domain)
			}
			seen[dial.Dial] = true
			upstreams = append(upstreams, dial)
		}
		routes = append(routes, AppRoute{AppID: fmt.Sprintf("%04d", i), Domain: r.Domain, Upstreams: upstreams})
	}
	return BuildConfig(BuildOptions{Admin: Admin{Listen: "fd/3", Origins: []string{"localhost"}}, Routes: routes})
}

func validDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") || domain != strings.ToLower(domain) {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	_, err := netip.ParseAddr(domain)
	return err != nil
}

// SafeRoutes validates every representable field, then exports only the typed
// route capability. It also rejects drift into unsupported handlers/settings.
func SafeRoutes(cfg *Config) (RouteSet, error) {
	set := RouteSet{Routes: []ProxyRoute{}}
	if cfg == nil {
		return set, fmt.Errorf("nil proxy config")
	}
	if cfg.Apps != nil {
		if cfg.Apps.HTTP == nil || len(cfg.Apps.HTTP.Servers) != 1 {
			return set, fmt.Errorf("unsupported proxy server configuration")
		}
		srv := cfg.Apps.HTTP.Servers[serverName]
		if srv == nil {
			return set, fmt.Errorf("unsupported proxy server")
		}
		for _, r := range srv.Routes {
			if len(r.Match) != 1 || len(r.Match[0].Host) != 1 || len(r.Handle) != 2 || r.Handle[0].Handler != "encode" || r.Handle[1].Handler != "reverse_proxy" {
				return set, fmt.Errorf("unsupported proxy route/handler")
			}
			route := ProxyRoute{Domain: r.Match[0].Host[0]}
			for _, u := range r.Handle[1].Upstreams {
				addr, err := netip.ParseAddrPort(u.Dial)
				if err != nil {
					return set, fmt.Errorf("invalid proxy dial address")
				}
				route.Upstreams = append(route.Upstreams, ProxyUpstream{IP: addr.Addr().String(), Port: uint32(addr.Port())})
			}
			set.Routes = append(set.Routes, route)
		}
	}
	fixed, err := FixedConfig(set)
	if err != nil {
		return set, err
	}
	if !reflect.DeepEqual(cfg, fixed) {
		return set, fmt.Errorf("proxy config contains unsupported admin, ports, logging or handler options")
	}
	return set, nil
}

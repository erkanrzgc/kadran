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
	// Redirects, Routes içindeki bir domaine 308 ile yönlenen adlardır.
	// omitempty ZORUNLU: yönlendirmesiz küme bugünkü JSON'u bayt bayt
	// üretmeli (eski aracı DisallowUnknownFields kullanıyor).
	//
	// Kanonik sıra: Routes sırasına göre gruplu, grup içinde girdi sırası.
	Redirects []ProxyRedirect `json:"redirects,omitempty"`
}

// ProxyRedirect, From adından To domainine giden 308 yönlendirmesidir.
type ProxyRedirect struct {
	From string `json:"from"`
	To   string `json:"to"`
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
const MaxRedirects = 1024

// FixedConfig constructs the only configuration the broker can load. Docker
// must use the same 172.16/12 pool permitted by kadrand's network sandbox.
func FixedConfig(set RouteSet) (*Config, error) {
	if set.Routes == nil {
		return nil, fmt.Errorf("proxy routes array is required")
	}
	if len(set.Routes) > MaxRoutes {
		return nil, fmt.Errorf("too many proxy routes")
	}
	if len(set.Redirects) > MaxRedirects {
		return nil, fmt.Errorf("too many proxy redirects")
	}
	routes := make([]AppRoute, 0, len(set.Routes))
	routeIndex := make(map[string]int, len(set.Routes))
	for i, r := range set.Routes {
		if !ValidDomain(r.Domain) {
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
			if isDockerNetworkInfrastructure(addr) {
				return nil, fmt.Errorf("upstream must be a container address, not a Docker network, gateway or broadcast address")
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
		routeIndex[r.Domain] = i
	}
	// kadrand güvenilmez: hedef kümedeki bir domain olmalı, kaynak hiçbir
	// domaine eşit olmamalı, kaynak tekrar etmemeli. Girdi sırası grup
	// içinde korunur (kanonik sıra, bkz. RouteSet.Redirects).
	seenFrom := make(map[string]bool, len(set.Redirects))
	for _, rd := range set.Redirects {
		if !ValidDomain(rd.From) || !ValidDomain(rd.To) {
			return nil, fmt.Errorf("invalid proxy redirect %q -> %q", rd.From, rd.To)
		}
		idx, ok := routeIndex[rd.To]
		if !ok {
			return nil, fmt.Errorf("redirect target %q is not a routed domain", rd.To)
		}
		if _, routed := routeIndex[rd.From]; routed {
			return nil, fmt.Errorf("redirect source %q is a routed domain", rd.From)
		}
		if seenFrom[rd.From] {
			return nil, fmt.Errorf("duplicate redirect source %q", rd.From)
		}
		seenFrom[rd.From] = true
		routes[idx].Redirects = append(routes[idx].Redirects, rd.From)
	}
	return BuildConfig(BuildOptions{Admin: Admin{Listen: "fd/3", Origins: []string{"localhost"}}, Routes: routes})
}

// isDockerNetworkInfrastructure, Docker köprü ağının ağ adresini (x.y.0.0),
// ağ geçidini (x.y.0.1 = HOST'un kendisi) ve yayın adresini (x.y.255.255)
// tanır. Docker'ın varsayılan havuzu /16 bloklara bölünüyor; bu üç adres hiçbir
// zaman konteynere verilmez, yani upstream olmaları yalnızca ele geçmiş bir
// kadrand'ın Caddy'yi host'a yönlendirmesine yarar (B2/I6).
//
// Bilinçli sınır: yalnızca /16 granülünü bilir. Daha küçük özel havuzların
// (ör. /24) ağ geçidi x.y.z.1 burada yakalanmaz; o, "hangi alt ağlar geçerli"
// ürün kararına bağlı (bkz. SECURITY.md).
func isDockerNetworkInfrastructure(addr netip.Addr) bool {
	if !addr.Is4() {
		return false
	}
	b := addr.As4()
	low := uint16(b[2])<<8 | uint16(b[3])
	return low == 0 || low == 1 || low == 0xFFFF
}

// ValidDomain, vekilin kabul ettiği alan adı kuralının TEK tanımıdır.
// API (create/update) ve proxydrv (FixedConfig) aynı işlevi kullanır: iki
// ayrı doğrulayıcı, API'nin kabul ettiği ama vekilin tüm rota kümesini
// reddettiği uygulamalar üretiyordu. Küçük harf, en az bir nokta, etiket
// başına en fazla 63 karakter ve IP sabiti DEĞİL.
func ValidDomain(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") || domain != strings.ToLower(domain) {
		return false
	}
	for _, label := range strings.Split(domain, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	_, err := netip.ParseAddr(domain)
	return err != nil
}

// redirectOf, rotanın TAM yönlendirme şekline uyup uymadığını söyler: tek
// eşleşme, tek Host, tek static_response işleyicisi, tek "Location" başlığı,
// tek değer, "https://" öneki ve "{http.request.uri}" soneki. Şekle uyup
// fazladan alan taşıyan rotayı (durum kodu, gövde) SafeRoutes sonundaki
// FixedConfig + DeepEqual yakalar; şekle hiç uymayan rota eski kontrole düşüp
// reddedilir.
func redirectOf(r Route) (from, to string, ok bool) {
	if len(r.Match) != 1 || len(r.Match[0].Host) != 1 || len(r.Handle) != 1 || r.Handle[0].Handler != "static_response" {
		return "", "", false
	}
	h := r.Handle[0]
	loc, found := h.Headers["Location"]
	if len(h.Headers) != 1 || !found || len(loc) != 1 {
		return "", "", false
	}
	const prefix, suffix = "https://", "{http.request.uri}"
	if len(loc[0]) < len(prefix)+len(suffix) || !strings.HasPrefix(loc[0], prefix) || !strings.HasSuffix(loc[0], suffix) {
		return "", "", false
	}
	return r.Match[0].Host[0], loc[0][len(prefix) : len(loc[0])-len(suffix)], true
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
			if from, to, ok := redirectOf(r); ok {
				set.Redirects = append(set.Redirects, ProxyRedirect{From: from, To: to})
				continue
			}
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

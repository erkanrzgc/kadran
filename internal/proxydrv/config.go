// Package proxydrv, Kadran'ın ters vekilini (kadran-caddy) yönetir.
//
// Yapılandırma SQLite'taki durumdan ÜRETİLİR ve Caddy'nin admin API'sine
// bütün olarak yüklenir. Caddy tarafında kısmi güncelleme yapılmıyor:
// gerçeğin kaynağı kontrol düzlemi, Caddy ise onun bir yansıması. Geri
// alma da bu yüzden basit — önceki durumdan üretilen JSON tekrar
// yüklenir.
package proxydrv

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
)

// ── Caddy yapılandırmasının KULLANILAN alt kümesi ────────────────────
//
// Caddy'nin tam şeması alınmıyor; yalnızca ürettiğimiz alanlar tiplendi.
// Gerekçe dockerdrv'deki `listEntry` ile aynı: dar bir yapı, tanımadığımız
// bir alanın ileride sessizce anlam kazanmasını engelliyor.
//
// ⚠ `Handler` serbest bir map DEĞİL. Serbest olsaydı, çağıran istediği
// Caddy modülünü ve seçeneğini yazabilirdi — oysa bu paketin var olma
// sebeplerinden biri, üretilebilecek yapılandırmayı KAPALI bir kümeye
// hapsetmek. The separate proxyguard broker enforces this outside kadrand;
// Caddy's core still supports file placeholders, so module exclusions alone
// must never be treated as an authorization boundary.

// Config, Caddy'ye yüklenen kök nesnedir.
type Config struct {
	Admin   Admin    `json:"admin"`
	Logging *Logging `json:"logging,omitempty"`
	Apps    *Apps    `json:"apps,omitempty"`
}

// Admin, yönetim ucunun ayarlarıdır.
//
// ⚠ HER yapılandırmada BULUNMALIDIR. Caddy `POST /load` ile kök nesnenin
// TAMAMINI değiştiriyor; admin bloğu olmayan bir yapılandırma yüklenirse
// Caddy varsayılana (localhost:2019 TCP) döner ve kadrand unix soketi
// üzerinden bir daha ULAŞAMAZ — yani sistem kendini kilitler.
// buildConfig bunu zorunlu kılıyor.
type Admin struct {
	Listen string `json:"listen"`
	// Origins, admin API'nin kabul ettiği Host başlıklarıdır. Boş
	// bırakılırsa Caddy her isteği 403 "host not allowed" ile reddeder
	// (ölçüldü) — yani bu alan da taşıyıcıdır.
	Origins []string `json:"origins,omitempty"`
}

type Logging struct {
	Logs map[string]LogConfig `json:"logs,omitempty"`
}

type LogConfig struct {
	Level string `json:"level,omitempty"`
}

type Apps struct {
	HTTP *HTTPApp `json:"http,omitempty"`
}

type HTTPApp struct {
	Servers map[string]*HTTPServer `json:"servers"`

	// HTTPPort ve HTTPSPort, Caddy'ye "hangi port düz HTTP, hangisi
	// HTTPS" der. Otomatik HTTPS bu bilgiyle hem ACME HTTP-01
	// dinleyicisini hem HTTP→HTTPS yönlendirmesini kurar.
	//
	// Varsayılan portlarda (80/443) yazılmıyor: Caddy zaten bunları
	// varsayıyor ve gereksiz alan, "yüklediğim şey canlıda mı"
	// karşılaştırmasına gürültü katar.
	HTTPPort  uint32 `json:"http_port,omitempty"`
	HTTPSPort uint32 `json:"https_port,omitempty"`
}

type HTTPServer struct {
	Listen []string `json:"listen"`
	Routes []Route  `json:"routes,omitempty"`
}

type Route struct {
	Match  []Match   `json:"match,omitempty"`
	Handle []Handler `json:"handle"`
	// Terminal, eşleşen isteğin sonraki rotalara düşmesini engeller.
	Terminal bool `json:"terminal,omitempty"`
}

type Match struct {
	Host []string `json:"host,omitempty"`
}

// Handler, üretilebilen işleyicilerin KAPALI kümesidir.
//
// Tek bir yapıda birleşiyorlar çünkü Caddy onları `handler` alanına göre
// ayırt ediyor. `omitempty` sayesinde kullanılmayan alanlar JSON'a hiç
// girmiyor.
type Handler struct {
	Handler string `json:"handler"`

	// reverse_proxy
	Upstreams []Upstream `json:"upstreams,omitempty"`

	// static_response
	StatusCode int    `json:"status_code,omitempty"`
	Body       string `json:"body,omitempty"`
	// Headers yalnız yönlendirme rotasında kullanılır (Location). SafeRoutes
	// başka bir başlığı ya da şablonu KABUL ETMEZ.
	Headers map[string][]string `json:"headers,omitempty"`

	// encode
	//
	// Encodings, Caddy'nin "hangi kodlayıcıları kullanabilirsin"
	// listesidir; değerler kodlayıcı yapılandırmasıdır ve varsayılanlar
	// bizim için doğru olduğundan boş nesne geçiliyor.
	//
	// ⚠ Yalnızca özel derlememizde BULUNAN kodlayıcılar yazılabilir.
	// Olmayan bir kodlayıcı istenirse Caddy yapılandırmanın TAMAMINI
	// reddeder ve o an canlı olan bütün rotalar düşer.
	Encodings map[string]struct{} `json:"encodings,omitempty"`
	Prefer    []string            `json:"prefer,omitempty"`
}

// Upstream, vekilin bağlanacağı arka uçtur.
//
// ⚠ `Dial` ÇAĞIRANDAN ALINMAZ; NewUpstream tarafından KURULUR.
// Serbest bir dize olsaydı, ele geçirilmiş bir kadrand oraya
// `unix//run/kadran-exec/exec.sock` yazıp ayrıcalıklı executor'ı
// internete açabilirdi. (Bugün ayrıca dosya izinleri de engelliyor —
// Caddy `kadran` grubunda değil — ama iki savunma bir savunmadan iyi ve
// bu, K-050'nin yazılı yükümlülüğü.)
type Upstream struct {
	Dial string `json:"dial"`
}

// NewUpstream, doğrulanmış bir IP ve porttan arka uç adresi KURAR.
//
// IP `netip.ParseAddr` ile ayrıştırılıyor: sonuç gerçekten bir adres
// değilse hata döner. Böylece `unix//…`, `localhost`, bir DNS adı veya
// şema taşıyan bir dize buradan GEÇEMEZ — temsil edilemez.
func NewUpstream(ip string, port uint32) (Upstream, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return Upstream{}, fmt.Errorf(
			"upstream address must be an IP (%q) — names, schemes or socket paths are not accepted", ip)
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	if !addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.Zone() != "" {
		return Upstream{}, fmt.Errorf("upstream address must be a non-loopback unicast IP (%q)", ip)
	}
	if port == 0 || port > 65535 {
		return Upstream{}, fmt.Errorf("upstream port must be between 1 and 65535 (%d)", port)
	}
	// netip.AddrPort, IPv6'yı köşeli parantezle doğru biçimlendirir.
	return Upstream{Dial: netip.AddrPortFrom(addr, uint16(port)).String()}, nil
}

// AppRoute, tek bir uygulamanın trafik tanımıdır.
type AppRoute struct {
	AppID     string
	Domain    string
	Upstreams []Upstream
	// Redirects, bu rotanın Domain alanına 308 ile yönlenen adlardır (verilen
	// sırayla). Her ad geçerli bir alan adı olmalı ve hiçbir domaine ya da
	// başka bir yönlendirmeye eşit olmamalı; aksi hâlde BuildConfig hata verir.
	Redirects []string
}

// redirectStatus, yönlendirme rotasının TEK durum kodu: yöntemi ve gövdeyi
// koruyan kalıcı yönlendirme.
const redirectStatus = 308

// redirectLocation, yönlendirmenin TEK Location şablonudur. Değişken kısım
// yalnız {http.request.uri}; build/caddy/redirect_test.go bunun istek
// yolundaki yer tutucuları yeniden açmadığını ölçer. to doğrulanmış bir
// alan adıdır.
func redirectLocation(to string) string {
	return "https://" + to + "{http.request.uri}"
}

// redirectRoute, from adından to domainine giden TEK yönlendirme rotasıdır;
// from ve to doğrulanmış olmalı.
func redirectRoute(from, to string) Route {
	return Route{
		Match: []Match{{Host: []string{from}}},
		Handle: []Handler{{
			Handler:    "static_response",
			StatusCode: redirectStatus,
			Headers:    map[string][]string{"Location": {redirectLocation(to)}},
		}},
		Terminal: true,
	}
}

// BuildOptions, üretilecek yapılandırmanın girdileridir.
type BuildOptions struct {
	Admin  Admin
	Routes []AppRoute
	// HTTPPort ve HTTPSPort sıfır bırakılırsa Caddy'nin varsayılanları
	// (80/443) geçerli olur.
	//
	// Sunucu YALNIZCA HTTPS portunu dinler; HTTPPort, Caddy'nin kendi
	// kurduğu yönlendirme/ACME dinleyicisi içindir. Gerekçe BuildConfig
	// içindeki nottadır.
	HTTPPort  uint32
	HTTPSPort uint32
}

var errNoAdmin = errors.New(
	"the admin block is required — a config loaded without it locks kadrand out of Caddy for good")

// BuildConfig, uygulama tanımlarından tam Caddy yapılandırmasını üretir.
//
// Sıralama BELİRLENİMLİ: rotalar app_id'ye göre sıralanıyor. Aksi hâlde
// aynı durumdan iki farklı JSON çıkar ve "yüklediğim şey canlıda mı"
// karşılaştırması gürültüye boğulurdu.
func BuildConfig(opts BuildOptions) (*Config, error) {
	if opts.Admin.Listen == "" {
		return nil, errNoAdmin
	}

	cfg := &Config{
		Admin: opts.Admin,
		Logging: &Logging{
			Logs: map[string]LogConfig{"default": {Level: "INFO"}},
		},
	}
	if len(opts.Routes) == 0 {
		return cfg, nil
	}

	routes := append([]AppRoute(nil), opts.Routes...)
	sort.Slice(routes, func(i, j int) bool { return routes[i].AppID < routes[j].AppID })

	out := make([]Route, 0, len(routes))
	seen := make(map[string]string, len(routes))
	for _, r := range routes {
		if r.Domain == "" {
			// Alan adı olmayan uygulama geçerli — yalnızca iç ağdan
			// erişilir ve vekilde hiç görünmez.
			continue
		}
		if prev, dup := seen[r.Domain]; dup {
			return nil, fmt.Errorf(
				"domain %q is assigned to two apps (%s and %s) — "+
					"which one wins would depend on order", r.Domain, prev, r.AppID)
		}
		seen[r.Domain] = r.AppID

		if len(r.Upstreams) == 0 {
			return nil, fmt.Errorf(
				"no upstream for %s — a route that sends traffic nowhere "+
					"would silently turn the app into a 502", r.AppID)
		}
		out = append(out, Route{
			Match: []Match{{Host: []string{r.Domain}}},
			Handle: []Handler{
				// ⚠ SIRA TAŞIYICI: encode, reverse_proxy'den ÖNCE.
				// Sonra gelseydi hiç çalışmazdı — vekil yanıtı çoktan
				// yazmış olurdu.
				encodeHandler(),
				{Handler: "reverse_proxy", Upstreams: r.Upstreams},
			},
			Terminal: true,
		})
	}

	if len(out) == 0 {
		return cfg, nil
	}

	// Yönlendirmeler domain rotalarından SONRA: önce bütün domainler
	// bilinmeli ki bir ad herhangi bir domaine eşitse HATA verilebilsin.
	for _, r := range routes {
		if r.Domain == "" {
			continue
		}
		for _, from := range r.Redirects {
			if !ValidDomain(from) || !ValidDomain(r.Domain) {
				return nil, fmt.Errorf("invalid redirect %q -> %q", from, r.Domain)
			}
			if prev, dup := seen[from]; dup {
				return nil, fmt.Errorf(
					"redirect name %q is already used by %s — "+
						"which one wins would depend on order", from, prev)
			}
			seen[from] = r.AppID
			out = append(out, redirectRoute(from, r.Domain))
		}
	}

	app := &HTTPApp{Servers: map[string]*HTTPServer{
		serverName: {
			// ⚠ YALNIZCA HTTPS portu — düz HTTP portu KASTEN yok.
			//
			// Tek sunucu iki portu da dinlediğinde Caddy o sunucu için
			// otomatik HTTP→HTTPS yönlendirmesi eklemez ve rotalar düz
			// HTTP üzerinde de yanıt verir. Gerçek sunucuda ölçüldü:
			// site geçerli sertifikayla HTTPS 200 dönerken aynı içeriği
			// http:// üzerinden de ŞİFRESİZ 200 ile veriyordu.
			//
			// Portu bırakmak yetenek kaybı değil: Caddy kendi :80
			// sunucusunu kuruyor ve o sunucu hem ACME HTTP-01
			// doğrulamasını karşılıyor hem yönlendirmeyi yapıyor.
			Listen: []string{
				":" + strconv.FormatUint(uint64(portOr(opts.HTTPSPort, 443)), 10),
			},
			Routes: out,
		},
	}}

	// Varsayılan dışı portlar Caddy'ye AYRICA söylenmeli: yönlendirmenin
	// hedefini bu alanlardan hesaplıyor. Boş bırakılsaydı 443'ü varsayar
	// ve yanlış porta yönlendirirdi — üstelik sessizce, çünkü HTTPS
	// tarafı yine çalışırdı.
	app.HTTPPort = opts.HTTPPort
	app.HTTPSPort = opts.HTTPSPort

	cfg.Apps = &Apps{HTTP: app}
	return cfg, nil
}

// encodeHandler, yanıtları istemciye giderken sıkıştırır.
//
// ⚠ Neden ZORUNLU: Caddy'nin vekil taşıyıcısı upstream'den gzip isteyip
// yanıtı ŞEFFAF biçimde açıyor. Bu işleyici olmasaydı, upstream düzgün
// sıkıştırma yapsa bile Caddy açtığı gövdeyi DÜZ METİN gönderirdi.
// Gerçek sunucuda ölçüldü: aynı JS paketi doğrudan nginx'ten gzip'li
// gelirken Caddy üzerinden 311929 bayt sıkıştırmasız geliyordu.
//
// brotli YOK: stok Caddy'de bulunmuyor, eklenti gerektiriyor. zstd
// muadili sıkıştırma oranı veriyor ve derlememizde MEVCUT (ölçüldü:
// `kadran-caddy list-modules` → http.encoders.gzip, http.encoders.zstd).
//
// `prefer` sırası belirlenimli: aksi hâlde aynı istemciye iki farklı
// kodlama dönebilir ve ara önbellekler Vary hesabını şaşırırdı.
func encodeHandler() Handler {
	return Handler{
		Handler:   "encode",
		Encodings: map[string]struct{}{"zstd": {}, "gzip": {}},
		Prefer:    []string{"zstd", "gzip"},
	}
}

// serverName, ürettiğimiz tek HTTP sunucusunun adı.
//
// Sabit: ad üretilebilir olsaydı, iki yükleme arasında sunucu adı değişip
// eski sunucu ortada kalabilirdi.
const serverName = "kadran"

func portOr(v, fallback uint32) uint32 {
	if v == 0 {
		return fallback
	}
	return v
}

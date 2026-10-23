package proxydrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"reflect"
	"sort"
	"time"
)

// Client connects to the typed proxy broker. NewAdmin is reserved for the
// isolated broker process, whose identity can reach Caddy's raw admin socket.
type Client struct {
	http  *http.Client
	admin bool
}

const (
	// adminHost, admin API'ye gönderilen Host başlığıdır.
	//
	// Caddy Host'u doğruluyor ve yapılandırmadaki `origins` listesinde
	// olmayan bir değerle gelen isteği 403 "host not allowed" ile
	// reddediyor (ölçüldü). Bu sabit, BuildConfig'in ürettiği
	// `Admin.Origins` ile aynı olmak zorunda.
	adminHost = "localhost"

	// requestTimeout, admin çağrıları için üst sınır.
	//
	// Burada sabit bir sınır GÜVENLİ: bu uçlar akış DEĞİL, istek/yanıt
	// turu. K-044'ün yasağı akan uçlar içindi ve buna uygulanmaz.
	requestTimeout = 15 * time.Second

	maxAdminResponse = 8 << 20
)

// New, unix soketi üzerinden konuşan bir istemci kurar.
//
// kadrand dilim 4c'de sağlık yoklaması için AF_INET aldı, ama
// `IPAddressDeny=any` + `IPAddressAllow=172.16.0.0/12` yalnızca Docker'ın
// özel ağına izin veriyor. Caddy'nin admin ucu HOSTTA duruyor, o aralıkta
// değil — yani TCP admin portu kullanılsaydı kadrand ona ULAŞAMAZDI
// (gerçek sunucuda ölçüldü: 127.0.0.1'e bağlanma denemesi engelleniyor).
//
// Unix soketi bu yüzden bir tercih değil, gereklilik. Ve iyi ki öyle:
// TCP admin ucu, hostta çalışan HERKESİN Caddy'yi yeniden yapılandırması
// demekti.
func New(socketPath string) *Client {
	return &Client{http: &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}}
}

// NewAdmin is for the separate broker process. The daemon's Unix permissions
// prohibit connecting to this raw admin socket, even after compromise.
func NewAdmin(socketPath string) *Client {
	c := New(socketPath)
	c.admin = true
	return c
}

// Load, yapılandırmayı yükler ve YÜKLENDİĞİNİ DOĞRULAR.
//
// ── Neden geri okuma ────────────────────────────────────────────────
//
// `POST /load`'ın 200 dönmesi, canlı yapılandırmanın gönderdiğimiz şey
// olduğunu KANITLAMAZ. Admin soketine root olarak çalışan
// başka bir süreç de yazabilir ve kontrol düzlemi bunu göremez; o zaman
// SQLite'taki "gerçeğin kaynağı", canlı olmayan bir şeyi tarif eder.
//
// Bu, projedeki pozitif-kanıt zincirinin ters vekildeki halkası:
// `aux` karesi → `image_id` → `DeploySucceeded` → ve burada
// "istediğim durumu geri okudum". Hiçbiri "hata almadım" demiyor.
func (c *Client) Load(ctx context.Context, cfg *Config) error {
	if !c.admin {
		set, err := SafeRoutes(cfg)
		if err != nil {
			return fmt.Errorf("proxy broker: %w", err)
		}
		body, err := json.Marshal(set)
		if err != nil {
			return err
		}
		if len(body) > MaxRouteRequest {
			return fmt.Errorf("proxy broker: route request too large")
		}
		raw, err := c.do(ctx, http.MethodPost, "/v1/routes", body)
		if err != nil {
			return err
		}
		live, err := decodeRoutes(raw)
		if err != nil {
			return err
		}
		if verificationErr := verifyApplied(cfg, live); verificationErr != nil {
			return fmt.Errorf("proxy broker: loaded config NOT FOUND live: %w", verificationErr)
		}
		return nil
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("caddy: could not encode the config: %w", err)
	}

	if _, err := c.do(ctx, http.MethodPost, "/load", body); err != nil {
		return err
	}

	live, err := c.Current(ctx)
	if err != nil {
		return fmt.Errorf("caddy: could not verify after loading: %w", err)
	}
	if err := verifyApplied(cfg, live); err != nil {
		return fmt.Errorf("caddy: the loaded config was NOT FOUND live: %w", err)
	}
	return nil
}

// Current, canlı yapılandırmayı okur.
func (c *Client) Current(ctx context.Context) (*Config, error) {
	if !c.admin {
		raw, err := c.do(ctx, http.MethodGet, "/v1/routes", nil)
		if err != nil {
			return nil, err
		}
		return decodeRoutes(raw)
	}
	raw, err := c.do(ctx, http.MethodGet, "/config/", nil)
	if err != nil {
		return nil, err
	}
	return decodeLive(raw)
}

// decodeLive, Caddy'nin `GET /config/` cevabını KATI çözer.
//
// Go yapılarında karşılığı olmayan her alan hata: SafeRoutes canlıyı
// FixedConfig ile DeepEqual karşılaştırıyor ve bu ancak cevabın TAMAMI
// karşılaştırmaya girerse bir şey kanıtlar. Düz json.Unmarshal bilinmeyen
// alanı sessizce atıyordu; `abort`, `transport`, dosya okuyan bir
// `handle_response` ya da dosyaya yazan bir log `writer`'ı taşıyan canlı
// yapılandırma güvenli sayılıyordu (K-154).
//
// Bu, Caddy'nin kendisi alan eklemediği için mümkün: geri okumayı
// yüklenen ağaçtan üretiyor, varsayılan doldurmuyor. Açılış yapılandırması
// (deploy/caddy/config.json) da yalnız modellenmiş alanlar taşıyor; ikisi
// de testlerde. Yinelenen anahtar denetimi yok: Caddy cevabı bir
// haritadan kodluyor, aynı anahtar iki kez gelemez.
func decodeLive(raw []byte) (*Config, error) {
	// Caddy hiç yapılandırma yoksa "null" döner.
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return &Config{}, nil
	}

	var cfg Config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("caddy: could not decode the live config: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("caddy: trailing data after the live config")
	}
	return &cfg, nil
}

func decodeRoutes(raw []byte) (*Config, error) {
	var set RouteSet
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&set); err != nil {
		return nil, fmt.Errorf("proxy broker: invalid route response: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("proxy broker: trailing response data")
	}
	return FixedConfig(set)
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://"+adminHost+path, rdr)
	if err != nil {
		return nil, fmt.Errorf("caddy: could not build the request: %w", err)
	}
	req.Host = adminHost
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy: could not reach the admin socket: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(io.LimitReader(resp.Body, maxAdminResponse))
	if err != nil {
		return nil, fmt.Errorf("caddy: could not read the response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("caddy: %s %s → %s: %s",
			method, path, resp.Status, adminError(out))
	}
	return out, nil
}

// adminError, Caddy'nin hata gövdesinden okunabilir mesajı çıkarır.
func adminError(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	return string(body)
}

// verifyApplied, gönderdiğimiz rotaların canlıda bulunduğunu doğrular.
//
// ── Neden bayt karşılaştırması DEĞİL ────────────────────────────────
//
// Caddy yapılandırmayı normalleştiriyor: varsayılan alanlar ekliyor,
// bazılarını yeniden düzenliyor. Ham JSON'ları karşılaştırmak her
// yüklemede yanlış alarm verirdi ve yanlış alarm veren bir kontrol
// kapatılmaya mahkûmdur.
//
// Bunun yerine ANLAMSAL karşılaştırma: her beklenen rota için aynı alan
// adı ve aynı upstream kümesi canlıda var mı?
//
// ── Karşılaştırma İKİ YÖNLÜ ────────────────────────────────────────
//
// Yalnızca "gönderdiğim her rota canlıda var mı" diye sormak YETMEZ; o
// hâliyle canlıdaki FAZLA rotalar görünmez kalır. Oysa fazla rota tam da
// bu kontrolün yakalamak için var olduğu şeydir: POST /load kök nesnenin
// tamamını değiştirdiğine göre, göndermediğim bir rotanın canlıda olması
// admin soketine BAŞKA BİRİNİN yazdığı anlamına gelir.
//
// Tek yönlü hâli ayrıca sessiz bir dağıtım hatasını da kaçırırdı: tek
// uygulamadan üretilmiş bir yapılandırma yüklenirse diğer uygulamaların
// rotaları silinir, ama "benim rotam canlıda" kontrolü YEŞİL geçerdi.
//
// ⚠ Bu yönün yanlış alarm vermediği ÖLÇÜLDÜ (docs/decisions.md K-054):
// Caddy otomatik HTTPS için ürettiği yönlendirme rotalarını saklanan
// yapılandırmaya geri YAZMIYOR; `GET /config/` POST edileni döndürüyor.
// Yazsaydı bu kontrol her yüklemede patlar ve kapatılmaya mahkûm olurdu.
func verifyApplied(want, live *Config) error {
	wantRoutes := routesByHost(want)
	liveRoutes := routesByHost(live)

	for host, wantDials := range wantRoutes {
		liveDials, ok := liveRoutes[host]
		if !ok {
			return fmt.Errorf("no route for %q", host)
		}
		if !sameStrings(wantDials, liveDials) {
			return fmt.Errorf("upstreams of %q differ: sent %v, live %v",
				host, wantDials, liveDials)
		}
	}

	// Sıralama belirlenimli: harita gezintisi rastgele olduğundan, aksi
	// hâlde aynı bozukluk her koşuda başka bir alan adını suçlardı.
	var extra []string
	for host := range liveRoutes {
		if _, ok := wantRoutes[host]; !ok {
			extra = append(extra, host)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf(
			"live has route(s) that were NOT SENT: %v — another process wrote to "+
				"the admin socket or the config was built incompletely", extra)
	}
	return nil
}

// routesByHost, yapılandırmadan "alan adı → upstream adresleri" çıkarır.
// MissingHosts, want'ın yönlendirdiği ama live'da HİÇ rotası olmayan
// alan adlarını sıralı döndürür.
//
// Vekil izleyicisinin tetikleyicisi (K-055'in ikinci yarısı, K-112):
// Caddy yeniden başlayınca rotasız açılıyor ve kadrand bunu görmezse
// siteler kapalı kalıyor. Bilerek TEK YÖNLÜ: fazla rotalar ve upstream
// farkları burada görünmez.
//
// ⚠ Bu işlev artık onarımın TETİĞİ DEĞİL. deploy.Reconciler.Repair yüklemeye
// Matches (iki yönlü) ile karar veriyor; fazla/yabancı rota ve farklı
// upstream de yeniden yüklemeyle düzeltiliyor. Atlanan uygulamanın canlı
// rotası ise Repair'de önce taşınıyor (LiveUpstreams), yani iki yönlü karşılaştırma
// onu silmiyor. MissingHosts yalnızca "rotasını tamamen kaybetmiş alan
// adları" günlüğü ve RepairResult.Missing içindir.
func MissingHosts(want, live *Config) []string {
	liveRoutes := routesByHost(live)
	var missing []string
	for host := range routesByHost(want) {
		if _, ok := liveRoutes[host]; !ok {
			missing = append(missing, host)
		}
	}
	sort.Strings(missing)
	return missing
}

// Matches, live'ın want'la İKİ YÖNLÜ aynı olduğunu doğrular: Load'un
// geri okumasıyla aynı kontrol (verifyApplied, K-054).
//
// Vekil izleyicisi bir alarmı kapatmadan önce bunu soruyor (K-112).
// MissingHosts'un tek yönlülüğü tetik için doğru, "her şey yolunda" demek
// için YETMEZ: canlıda gönderilmemiş bir rota varken de eksik yoktur.
func Matches(want, live *Config) error {
	return verifyApplied(want, live)
}

// LiveUpstreams, live'da host'a giden upstream'lerden GEÇERLİ olanları
// döndürür.
//
// Onarım (deploy.Reconciler.Repair) atlanan, yani o an sağlıksız olan bir
// uygulamanın canlı rotasını korumak için kullanıyor. Canlı yapılandırma
// güvenilir kaynak değil — admin soketine başkası da yazabilir (K-054) —
// bu yüzden her adres NewUpstream'den yeniden geçiyor: soket yolu, ad ya
// da belirsiz adres taşınamaz.
func LiveUpstreams(live *Config, host string) []Upstream {
	var out []Upstream
	for _, dial := range routesByHost(live)[host] {
		ap, err := netip.ParseAddrPort(dial)
		if err != nil {
			continue
		}
		u, err := NewUpstream(ap.Addr().String(), uint32(ap.Port()))
		if err != nil {
			continue
		}
		out = append(out, u)
	}
	return out
}

// LiveRedirects, live içinde to domainine yönlenen KAYNAK adları rota
// sırasıyla döndürür.
//
// Onarım, atlanan (sağlıksız) uygulamanın canlı yönlendirmelerini taşımak
// için kullanır (LiveUpstreams'in yönlendirme karşılığı). Canlı yapılandırma
// güvenilir kaynak değil: yalnız aracının üreteceği TAM rota (redirectRoute)
// ve geçerli adlar kabul edilir, kurcalanmış rota taşınmaz.
func LiveRedirects(live *Config, to string) []string {
	var out []string
	if live == nil || live.Apps == nil || live.Apps.HTTP == nil {
		return out
	}
	for _, srv := range live.Apps.HTTP.Servers {
		if srv == nil {
			continue
		}
		for _, r := range srv.Routes {
			from, target, ok := redirectOf(r)
			if !ok || target != to || !ValidDomain(from) || !ValidDomain(target) {
				continue
			}
			if reflect.DeepEqual(r, redirectRoute(from, target)) {
				out = append(out, from)
			}
		}
	}
	return out
}

func routesByHost(cfg *Config) map[string][]string {
	out := map[string][]string{}
	if cfg == nil || cfg.Apps == nil || cfg.Apps.HTTP == nil {
		return out
	}
	for _, srv := range cfg.Apps.HTTP.Servers {
		if srv == nil {
			continue
		}
		for _, r := range srv.Routes {
			dials := []string{}
			for _, h := range r.Handle {
				for _, u := range h.Upstreams {
					dials = append(dials, u.Dial)
				}
				// Yönlendirme rotasının "hedefi" Location şablonudur; verifyApplied
				// hedef kaymasını da görsün. Adres olmadığından LiveUpstreams
				// bunu eler.
				if h.Handler == "static_response" {
					for _, loc := range h.Headers["Location"] {
						dials = append(dials, "redirect "+loc)
					}
				}
			}
			for _, m := range r.Match {
				for _, host := range m.Host {
					out[host] = append(out[host], dials...)
				}
			}
		}
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Küçük kümeler; sıralamadan bağımsız karşılaştırma için sayım.
	count := make(map[string]int, len(a))
	for _, s := range a {
		count[s]++
	}
	for _, s := range b {
		count[s]--
		if count[s] < 0 {
			return false
		}
	}
	return true
}

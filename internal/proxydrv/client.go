package proxydrv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sort"
	"time"
)

// Client, Caddy'nin admin API'sine bağlanır.
type Client struct {
	http *http.Client
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
// panelyd `RestrictAddressFamilies=AF_UNIX` ile çalışıyor, yani TCP
// açamaz — Caddy'nin admin ucunun unix soketi olması bu yüzden bir
// tercih değil, gereklilik.
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

// Load, yapılandırmayı yükler ve YÜKLENDİĞİNİ DOĞRULAR.
//
// ── Neden geri okuma ────────────────────────────────────────────────
//
// `POST /load`'ın 200 dönmesi, canlı yapılandırmanın gönderdiğimiz şey
// olduğunu KANITLAMAZ. Admin soketine `panely` (veya root) olarak çalışan
// başka bir süreç de yazabilir ve kontrol düzlemi bunu göremez; o zaman
// SQLite'taki "gerçeğin kaynağı", canlı olmayan bir şeyi tarif eder.
//
// Bu, projedeki pozitif-kanıt zincirinin ters vekildeki halkası:
// `aux` karesi → `image_id` → `DeploySucceeded` → ve burada
// "istediğim durumu geri okudum". Hiçbiri "hata almadım" demiyor.
func (c *Client) Load(ctx context.Context, cfg *Config) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("caddy: yapılandırma kodlanamadı: %w", err)
	}

	if _, err := c.do(ctx, http.MethodPost, "/load", body); err != nil {
		return err
	}

	live, err := c.Current(ctx)
	if err != nil {
		return fmt.Errorf("caddy: yükleme sonrası doğrulama yapılamadı: %w", err)
	}
	if err := verifyApplied(cfg, live); err != nil {
		return fmt.Errorf("caddy: yüklenen yapılandırma canlıda BULUNAMADI: %w", err)
	}
	return nil
}

// Current, canlı yapılandırmayı okur.
func (c *Client) Current(ctx context.Context) (*Config, error) {
	raw, err := c.do(ctx, http.MethodGet, "/config/", nil)
	if err != nil {
		return nil, err
	}
	// Caddy hiç yapılandırma yoksa "null" döner.
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return &Config{}, nil
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("caddy: canlı yapılandırma çözümlenemedi: %w", err)
	}
	return &cfg, nil
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
		return nil, fmt.Errorf("caddy: istek kurulamadı: %w", err)
	}
	req.Host = adminHost
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("caddy: admin soketine ulaşılamadı: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	out, err := io.ReadAll(io.LimitReader(resp.Body, maxAdminResponse))
	if err != nil {
		return nil, fmt.Errorf("caddy: yanıt okunamadı: %w", err)
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
			return fmt.Errorf("%q için rota yok", host)
		}
		if !sameStrings(wantDials, liveDials) {
			return fmt.Errorf("%q upstream'leri farklı: gönderilen %v, canlı %v",
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
			"canlıda GÖNDERİLMEYEN rota(lar) var: %v — admin soketine başka bir "+
				"süreç yazmış ya da yapılandırma eksik üretilmiş", extra)
	}
	return nil
}

// routesByHost, yapılandırmadan "alan adı → upstream adresleri" çıkarır.
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

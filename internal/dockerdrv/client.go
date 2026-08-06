// Package dockerdrv, Docker Engine API'sinin Panely'nin kullandığı KADARINI
// sarmalar.
//
// # Bu paket neden proto mesajı kabul etmiyor
//
// Buradaki hiçbir fonksiyon `*panelyv1.*` almaz. Bu kasıtlı bir kısıttır:
// doğrulama internal/exec'te, ayrıcalıklı hiçbir şey yapılmadan ÖNCE ve tek
// noktada olur. Eğer sürücü proto isteklerini kabul etseydi, ileride bir
// çağıran ona DOĞRULANMAMIŞ bir istek verebilirdi ve "önce doğrula" kuralı
// bir paket sınırında sessizce delinirdi.
//
// Bunun yerine sürücü dar, ayrıştırılmış argümanlar alır. Doğrulanmamış bir
// istek burada TEMSİL EDİLEMEZ — şemadaki `container_id` yokluğuyla aynı
// hamle, iç API'ye uygulanmış hâli.
//
// # Neden Docker SDK yok
//
// `github.com/docker/docker/client` devasa bir bağımlılık ağacı getirir ve
// bu kod AYRICALIKLI süreçte çalışır. Ayrıcalıklı yüzeyi denetlenebilir
// tutmak modelin dayandığı kural (K-002); kolaylık için ondan taviz
// verilmez. Engine API düz HTTP'dir.
package dockerdrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ════════════════════════════════════════════════════════════════════
//
//	ENGINE API SÜRÜMÜ: SABİT DEĞİL, UZLAŞILIR
//
// ════════════════════════════════════════════════════════════════════
//
// Sürüm bir dönem sabitti ve bu YANLIŞTI. İki ölçüm bunu tek günde
// gösterdi:
//
//	v1.51 (geliştirme sunucusuna bakılarak seçilmişti)
//	  → CI runner'ı: "client version 1.51 is too new.
//	                  Maximum supported API version is 1.48"
//	v1.41 (uyumlu olsun diye düşürüldü)
//	  → Docker 29.1.3: "client version 1.41 is too old.
//	                    Minimum supported API version is 1.44"
//
// Yani iki host ZIT kısıtlar dayatıyor. Daemon'ların desteklediği aralık
// bir PENCERE ve o pencere zamanla KAYIYOR — Docker tabanı bir kez
// 1.24'ten 1.44'e çekti. Sabit bir pin bugün çalışsa da ileride bir
// hostta tamamen kırılır ve arıza kısmi değil TOPLAM olur: her istek
// reddedilir.
//
// Bu yüzden sürüm ilk kullanımda UZLAŞILIYOR: daemon'ın bildirdiği
// [MinAPIVersion, ApiVersion] aralığı ile bizim sınandığımız aralık
// kesiştiriliyor ve ortak en yüksek sürüm seçiliyor.
//
// Sabitlemenin ASIL gerekçesi korunuyor: sürümsüz istek daemon'ın en
// yenisine düşer ve alan anlamları sürümler arasında değişebilir. Uzlaşma
// bunu bozmuyor — sürüm yine bağlantı başına SABİTLENİYOR, yalnızca hangi
// sürüm olduğu ölçülerek belirleniyor ve BİZİM üst sınırımızı aşamıyor.
const (
	// minAPIVersion, sınandığımız en düşük sürüm. Kullandığımız her alan
	// (Labels, SecurityOpt, NanoCpus, BlkioWeight, Binds, NetworkMode,
	// filtreli listeleme, ağ oluşturma) bundan çok daha eskidir; taban
	// daemon uyumu için var, alan ihtiyacı için değil.
	minAPIVersion = "1.44"

	// maxAPIVersion, sınandığımız en yüksek sürüm. Daemon daha yenisini
	// destekliyorsa bile bunun ötesine GEÇİLMEZ: sınanmamış bir sürümde
	// alan anlamlarının aynı kaldığı garanti değil.
	maxAPIVersion = "1.48"
)

// negotiate, daemon ile ortak en yüksek API sürümünü belirler.
//
// Yalnızca bir kez koşar. Başarısız olursa hata her çağrıda tekrar
// üretilir — sessizce sürümsüz isteğe DÜŞMEZ; o, tam olarak kaçınmak
// istediğimiz davranıştır.
func (c *Client) negotiate(ctx context.Context) (string, error) {
	c.once.Do(func() {
		var payload struct {
			APIVersion    string `json:"ApiVersion"`
			MinAPIVersion string `json:"MinAPIVersion"`
		}
		// `/version` sürüm öneki OLMADAN çağrılabilen tek uçtur; uzlaşmanın
		// mümkün olmasını sağlayan da budur.
		if err := c.getJSON(ctx, c.base+"/version", &payload); err != nil {
			c.negErr = err
			return
		}
		c.apiVersion, c.negErr = pickVersion(payload.MinAPIVersion, payload.APIVersion)
	})
	return c.apiVersion, c.negErr
}

// pickVersion, iki aralığı kesiştirip ortak en yüksek sürümü seçer.
func pickVersion(daemonMin, daemonMax string) (string, error) {
	if daemonMax == "" {
		return "", errors.New("docker: daemon API sürümünü bildirmedi")
	}
	// MinAPIVersion çok eski daemon'larda boş gelebilir; o durumda tabanı
	// bizimki belirler.
	lo := minAPIVersion
	if daemonMin != "" && compareVersions(daemonMin, lo) > 0 {
		lo = daemonMin
	}
	hi := maxAPIVersion
	if compareVersions(daemonMax, hi) < 0 {
		hi = daemonMax
	}
	if compareVersions(lo, hi) > 0 {
		return "", fmt.Errorf(
			"docker: ortak API sürümü yok — daemon [%s, %s], Panely [%s, %s]",
			daemonMin, daemonMax, minAPIVersion, maxAPIVersion)
	}
	return "v" + hi, nil
}

// compareVersions, "1.44" biçimindeki iki sürümü karşılaştırır.
//
// Sözlüksel karşılaştırma YANLIŞ olurdu: "1.9" > "1.48" der.
func compareVersions(a, b string) int {
	amaj, amin := splitVersion(a)
	bmaj, bmin := splitVersion(b)
	if amaj != bmaj {
		return amaj - bmaj
	}
	return amin - bmin
}

func splitVersion(v string) (major, minor int) {
	maj, min, _ := strings.Cut(strings.TrimPrefix(v, "v"), ".")
	major, _ = strconv.Atoi(maj)
	minor, _ = strconv.Atoi(min)
	return major, minor
}

// Client, tek bir Docker daemon'ına konuşur.
type Client struct {
	http *http.Client
	// base, isteklerin öneki. Unix soketinde ana bilgisayar adı anlamsızdır
	// ama http.Client geçerli bir URL ister; "docker" yalnızca yer tutucu.
	base string

	// once/apiVersion/negErr, sürüm uzlaşmasının sonucunu bir kez
	// hesaplayıp saklar.
	once       sync.Once
	apiVersion string
	negErr     error

	// volumeRoot, uygulama hacimlerinin kökü. İstekten ALINMAZ.
	//
	// Bu dizinin `nodev,nosuid` ile bağlanmış olması gerekir; bunu bir
	// systemd mount birimi yapar ve sürücü çalışma anında DOĞRULAR
	// (mountinfo.go). Sertleştirmenin varlığını varsaymak, tam olarak
	// K-039'da sessizce yanlış çıkan şeydir.
	volumeRoot string
}

// New, unix soketi üzerinden konuşan bir istemci kurar.
func New(socketPath, volumeRoot string) *Client {
	return &Client{
		http: &http.Client{
			// Akış uçları (logs, build) kendi bağlamlarıyla yönetilir; bu
			// zaman aşımı yalnızca istek/yanıt turları içindir.
			Timeout: 60 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
			},
		},
		base:       "http://docker",
		volumeRoot: volumeRoot,
	}
}

// apiError, daemon'ın döndürdüğü hatadır.
//
// Mesaj çağırana taşınır ama DENETİM KAYDINA GİRMEZ: içeriği kullanıcının
// deposundan/imajından gelir ve sır basabilir (bkz. exec katmanı).
type apiError struct {
	Status  int
	Message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("docker: HTTP %d: %s", e.Status, e.Message)
}

// do, isteği gönderir ve hata durumlarını apiError'a çevirir.
//
// Yanıt gövdesi ÇAĞIRANA AÇIK bırakılır (akış uçları için); kapatmak
// çağıranın sorumluluğudur.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body any) (*http.Response, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("docker: istek kodlanamadı: %w", err)
		}
		rdr = bytes.NewReader(b)
	}

	ver, err := c.negotiate(ctx)
	if err != nil {
		return nil, err
	}

	u := c.base + "/" + ver + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, fmt.Errorf("docker: istek kurulamadı: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: soketle konuşulamadı: %w", err)
	}
	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		return nil, &apiError{Status: resp.StatusCode, Message: decodeMessage(resp.Body)}
	}
	return resp, nil
}

// decodeMessage, Docker'ın hata gövdesindeki `message` alanını çıkarır.
func decodeMessage(r io.Reader) string {
	// Sınır: kötü davranan bir daemon'ın belleği doldurmasını engeller.
	b, err := io.ReadAll(io.LimitReader(r, 8<<10))
	if err != nil {
		return "yanıt okunamadı"
	}
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(b, &payload) == nil && payload.Message != "" {
		return payload.Message
	}
	return string(bytes.TrimSpace(b))
}

// doJSON, isteği gönderir ve JSON yanıtını out'a çözer. out nil olabilir.
func (c *Client) doJSON(ctx context.Context, method, path string, q url.Values, body, out any) error {
	resp, err := c.do(ctx, method, path, q, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("docker: yanıt çözümlenemedi: %w", err)
	}
	return nil
}

// getJSON, sürüm öneki OLMADAN bir URL'den JSON okur.
//
// Yalnızca uzlaşma için var: normal uçlar daima do/doJSON üzerinden,
// yani uzlaşılmış sürümle gider.
func (c *Client) getJSON(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("docker: istek kurulamadı: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("docker: soketle konuşulamadı: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		return &apiError{Status: resp.StatusCode, Message: decodeMessage(resp.Body)}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("docker: yanıt çözümlenemedi: %w", err)
	}
	return nil
}

// Ping, daemon'ın sürümünü döndürür ve sürüm uzlaşmasını tetikler.
//
// Salt okunur. Uzlaşma başarısızsa hata döner — böylece "Docker var mı"
// sorusunun cevabı "var ama konuşamıyoruz" durumunu da kapsar.
func (c *Client) Ping(ctx context.Context) (string, error) {
	if _, err := c.negotiate(ctx); err != nil {
		return "", err
	}
	var payload struct {
		Version string `json:"Version"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/version", nil, nil, &payload); err != nil {
		return "", err
	}
	return payload.Version, nil
}

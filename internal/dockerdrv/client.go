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
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// apiVersion, konuşulan Engine API sürümüdür.
//
// Sabitlenmesi gerekiyor: sürümsüz istekler daemon'ın EN YENİ sürümüne
// düşer ve alan anlamları sürümler arasında değişebilir. Ayrıcalıklı bir
// istemcinin gövde anlamını daemon güncellemesine bırakması kabul edilemez.
const apiVersion = "v1.51"

// Client, tek bir Docker daemon'ına konuşur.
type Client struct {
	http *http.Client
	// base, isteklerin öneki. Unix soketinde ana bilgisayar adı anlamsızdır
	// ama http.Client geçerli bir URL ister; "docker" yalnızca yer tutucu.
	base string

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

	u := c.base + "/" + apiVersion + path
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

// Ping, daemon'ın sürümünü döndürür. Salt okunur.
func (c *Client) Ping(ctx context.Context) (string, error) {
	var payload struct {
		Version string `json:"Version"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/version", nil, nil, &payload); err != nil {
		return "", err
	}
	return payload.Version, nil
}

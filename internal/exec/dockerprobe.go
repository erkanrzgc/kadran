package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// DefaultDockerSocket, Docker Engine API'sinin varsayılan unix soketidir.
const DefaultDockerSocket = "/run/docker.sock"

// dockerVersion, Docker Engine'in sürümünü sorgular.
//
// # Neden Docker SDK kullanılmıyor?
//
// `github.com/docker/docker/client` devasa bir bağımlılık ağacı getirir.
// Bu kod AYRICALIKLI süreçte çalışıyor ve K-002'nin değişmezi ayrıcalıklı
// yüzeyi denetlenebilir tutmayı gerektiriyor: "internal/exec 2000 satırı
// geçerse ne eklendiği sorgulanır."
//
// Engine API düz HTTP'dir; sürüm sorgusu 30 satır. Kolaylık için ayrıcalıklı
// binary'yi şişirmek, kuralın ilk teklifte çiğnenmesi olurdu.
//
// Faz 1'de konteyner işlemleri geldiğinde bu dosya büyüyecek — ama SDK'nın
// tamamını değil, gerçekten kullandığımız uç noktaları taşıyacak.
func dockerVersion(ctx context.Context, socketPath string) (string, error) {
	client := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	// Ana bilgisayar adı unix soketinde anlamsızdır ama http.Client
	// geçerli bir URL ister; "docker" yalnızca yer tutucudur.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/version", nil)
	if err != nil {
		return "", err
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("docker soketine ulaşılamadı: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("docker /version durum kodu %d döndürdü", resp.StatusCode)
	}

	var payload struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("docker sürüm yanıtı çözümlenemedi: %w", err)
	}
	return payload.Version, nil
}

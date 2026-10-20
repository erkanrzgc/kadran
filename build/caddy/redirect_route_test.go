package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

// kadran-proxy'nin ürettiği yönlendirme rotası (altın dosya) GERÇEK Caddy
// yapılandırmasında ve GERÇEK dinleyicide denenir: rota şekli, status_code ve
// Location şablonu burada ayrıştırılıp çalıştırılıyor. Kadran paketleri bu
// modüle alınmaz; altın dosya göreli yoldan okunur.
func TestRedirectRouteGoldenInRealCaddy(t *testing.T) {
	raw, err := os.ReadFile("../../internal/proxydrv/testdata/redirect-route.json")
	if err != nil {
		t.Fatal(err)
	}
	var route json.RawMessage
	if err := json.Unmarshal(raw, &route); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}

	cfg := fmt.Sprintf(`{
  "admin": {"disabled": true},
  "apps": {"http": {"servers": {"kadran": {
    "listen": [%q],
    "automatic_https": {"disable": true},
    "routes": [%s]
  }}}}
}`, addr, route)
	if err := caddy.Load([]byte(cfg), true); err != nil {
		t.Fatalf("Caddy yapılandırmayı yüklemedi: %v", err)
	}
	t.Cleanup(func() { _ = caddy.Stop() })

	client := &http.Client{
		Timeout:       5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	get := func(host string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://"+addr+"/a?b=1", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = resp.Body.Close() })
		return resp
	}

	resp := get("www.example.com")
	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("durum %d, beklenen 308", resp.StatusCode)
	}
	if got := resp.Header.Get("Location"); got != "https://example.com/a?b=1" {
		t.Fatalf("Location %q", got)
	}

	if other := get("other.example"); other.StatusCode == http.StatusPermanentRedirect || strings.Contains(other.Header.Get("Location"), "example.com") {
		t.Fatalf("başka Host yönlendirildi: %d %q", other.StatusCode, other.Header.Get("Location"))
	}
}

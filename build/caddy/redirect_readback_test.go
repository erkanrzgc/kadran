package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2"
)

// freeLoopbackAddr, 127.0.0.1 üzerinde boş bir port seçer.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	return addr
}

// proxydrv.SafeRoutes canlıdan GERİ OKUNAN yönlendirme rotasını FixedConfig'in
// kurduğuyla DeepEqual karşılaştırıyor. Bu ancak Caddy'nin admin API'si rotayı
// yüklendiği biçimle (anlamca) aynı geri veriyorsa doğru: alan eklerse, sayıyı
// dizeye çevirirse ya da başlığı yeniden adlandırırsa her geri okuma
// "desteklenmeyen yapılandırma" olur ve aracı yönlendirmeli kümeyi taşıyamaz.
// 20 Ekim'de bir kez elle ölçülmüştü; bu test o ölçümü kalıcı kılar (karar E1).
//
// Admin YALNIZ loopback'te dinler; persist kapalı, yani kullanıcının yapılandırma
// dizinine autosave yazılmaz.
func TestRedirectRouteGoldenReadsBackFromAdmin(t *testing.T) {
	raw, err := os.ReadFile("../../internal/proxydrv/testdata/redirect-route.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden any
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatal(err)
	}

	admin, listen := freeLoopbackAddr(t), freeLoopbackAddr(t)
	cfg := fmt.Sprintf(`{
  "admin": {"listen": %q, "config": {"persist": false}},
  "apps": {"http": {"servers": {"kadran": {
    "listen": [%q],
    "automatic_https": {"disable": true},
    "routes": [%s]
  }}}}
}`, admin, listen, raw)
	if err := caddy.Load([]byte(cfg), true); err != nil {
		t.Fatalf("Caddy yapılandırmayı yüklemedi: %v", err)
	}
	t.Cleanup(func() {
		// caddy.Stop admin sunucusunu kapatmıyor; admin'i kapatan yapılandırma
		// yüklenir, sonra uygulamalar durdurulur.
		_ = caddy.Load([]byte(`{"admin": {"disabled": true, "config": {"persist": false}}}`), true)
		_ = caddy.Stop()
	})

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + admin + "/config/apps/http/servers/kadran/routes")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin GET durumu %d: %s", resp.StatusCode, body)
	}

	var got any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("geri okunan rotalar JSON değil: %v\n%s", err, body)
	}
	if want := []any{golden}; !reflect.DeepEqual(got, want) {
		t.Fatalf("geri okunan rota altın dosyadan farklı:\n got: %s\nwant: [%s]", body, raw)
	}
}

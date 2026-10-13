//go:build linux

package proxyguard

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

func TestTypedClientUnixBrokerAndRawAdminIntegration(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "kadran-proxy-integration-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	start := func(name string, handler http.Handler) string {
		t.Helper()
		sock := filepath.Join(dir, name)
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: handler}
		go func() { _ = server.Serve(ln) }()
		t.Cleanup(func() { _ = server.Close() })
		return sock
	}
	var mu sync.Mutex
	var live proxydrv.Config
	loads := 0
	raw := start("raw.sock", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/load":
			loads++
			if err := json.NewDecoder(r.Body).Decode(&live); err != nil {
				http.Error(w, err.Error(), 400)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/config/":
			_ = json.NewEncoder(w).Encode(live)
		default:
			http.NotFound(w, r)
		}
	}))
	typed := start("typed.sock", Handler(proxydrv.NewAdmin(raw)))
	client := proxydrv.New(typed)
	var set proxydrv.RouteSet
	if err := json.Unmarshal([]byte(goodRoutes), &set); err != nil {
		t.Fatal(err)
	}
	cfg, err := proxydrv.FixedConfig(set)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Load(t.Context(), cfg); err != nil {
		t.Fatal(err)
	}
	current, err := client.Current(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got, err := proxydrv.SafeRoutes(current)
	if err != nil || len(got.Routes) != 1 {
		t.Fatalf("typed live readback: %+v %v", got, err)
	}
	attackClient := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", typed)
	}}}
	for _, attack := range []struct {
		path, body string
		status     int
	}{
		{"/load", `{"admin":{"listen":":2019"}}`, 404},
		{"/v1/routes", `{"routes":[],"handler":"static_response","body":"{file./synthetic/key}"}`, 400},
		{"/v1/routes", strings.Replace(goodRoutes, "172.18.0.3", "unix//run/kadran-exec/exec.sock", 1), 400},
	} {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://localhost"+attack.path, strings.NewReader(attack.body))
		if err != nil {
			t.Fatal(err)
		}
		resp, err := attackClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != attack.status {
			t.Errorf("attack status=%d want %d", resp.StatusCode, attack.status)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if loads != 1 {
		t.Fatalf("attacks reached raw Caddy %d times", loads-1)
	}
}

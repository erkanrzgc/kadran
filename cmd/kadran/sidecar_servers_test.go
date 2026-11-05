package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/servers"
)

type serversResult struct {
	Servers  []servers.Server `json:"servers"`
	Imported int              `json:"imported"`
}

// serversSidecar, kendi geçici listesi olan bir sidecar.
func serversSidecar(t *testing.T) *sidecar {
	t.Helper()
	c, _, _ := newTestCLI("")
	c.servers = servers.Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}
	return &sidecar{cli: c}
}

func callServers(t *testing.T, s *sidecar, method, params string) (serversResult, *rpcError) {
	t.Helper()
	res, rpcErr := s.dispatch(context.Background(), rpcRequest{Method: method, Params: json.RawMessage(params)})
	if rpcErr != nil {
		return serversResult{}, rpcErr
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	var out serversResult
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out, nil
}

func mustServers(t *testing.T, s *sidecar, method, params string) serversResult {
	t.Helper()
	out, rpcErr := callServers(t, s, method, params)
	if rpcErr != nil {
		t.Fatalf("%s %s: %+v", method, params, rpcErr)
	}
	return out
}

func TestSidecarServersRoundTrip(t *testing.T) {
	s := serversSidecar(t)
	if got := mustServers(t, s, "servers.list", `null`); len(got.Servers) != 0 {
		t.Fatalf("boş liste: %+v", got)
	}
	mustServers(t, s, "servers.add", `{"name":"hetzner","target":"kadran-client@a","provider":"Hetzner"}`)
	// CLI yönetici hedefi ekledi; masaüstünün düzenlemesi onu silmemeli.
	if _, err := s.cli.servers.Update(func(l []servers.Server) ([]servers.Server, error) {
		l[0].Admin, l[0].Sudo = "ops@a", true
		return l, nil
	}); err != nil {
		t.Fatal(err)
	}
	got := mustServers(t, s, "servers.update", `{"target":"kadran-client@a","name":"Almanya","provider":""}`)
	if g := got.Servers[0]; g.Name != "Almanya" || g.Provider != "" || g.Admin != "ops@a" || !g.Sudo {
		t.Fatalf("update: %+v", g)
	}
	if got := mustServers(t, s, "servers.list", `{}`); len(got.Servers) != 1 || got.Servers[0].Name != "Almanya" {
		t.Fatalf("list: %+v", got)
	}
	if got := mustServers(t, s, "servers.remove", `{"target":"kadran-client@a"}`); len(got.Servers) != 0 {
		t.Fatalf("remove: %+v", got)
	}
}

func TestSidecarServersRejectUnknownFields(t *testing.T) {
	s := serversSidecar(t)
	for _, tc := range []struct{ method, params string }{
		{"servers.add", `{"name":"a","target":"kadran-client@a","admin":"root@x"}`},
		{"servers.update", `{"target":"kadran-client@a","name":"a","provider":"","sudo":true}`},
		{"servers.remove", `{"target":"kadran-client@a","name":"a"}`},
		{"servers.list", `{"target":"x"}`},
		{"servers.import", `{"profiles":[],"force":true}`},
		{"servers.import", `{"profiles":[{"name":"a","target":"kadran-client@a","admin":"root@x"}]}`},
		{"servers.add", `null`},
	} {
		_, rpcErr := callServers(t, s, tc.method, tc.params)
		if rpcErr == nil || rpcErr.Code != codeInvalidParams {
			t.Errorf("%s %s: %+v", tc.method, tc.params, rpcErr)
		}
	}
	if list, _ := s.cli.servers.Load(); len(list) != 0 {
		t.Errorf("reddedilen istek yazdı: %+v", list)
	}
}

func TestSidecarServersImport(t *testing.T) {
	s := serversSidecar(t)
	mustServers(t, s, "servers.add", `{"name":"a","target":"kadran-client@a"}`)
	got := mustServers(t, s, "servers.import", `{"profiles":[
		{"name":"a","target":"kadran-client@a"},
		{"name":"a","target":"kadran-client@b","provider":"P"},
		{"name":"c","target":"kadran-client@c"}]}`)
	if got.Imported != 2 || len(got.Servers) != 3 || got.Servers[1].Name != "a (2)" || got.Servers[1].Provider != "P" {
		t.Fatalf("import: %+v", got)
	}
}

func TestSidecarServersDamagedFile(t *testing.T) {
	s := serversSidecar(t)
	body := `{"version":1,"servers":[`
	if err := os.MkdirAll(filepath.Dir(s.cli.servers.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.cli.servers.Path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ method, params string }{
		{"servers.list", `null`},
		{"servers.add", `{"name":"a","target":"kadran-client@a"}`},
		{"servers.import", `{"profiles":[{"name":"a","target":"kadran-client@a"}]}`},
	} {
		_, rpcErr := callServers(t, s, tc.method, tc.params)
		if rpcErr == nil || !strings.Contains(rpcErr.Message, "damaged") {
			t.Errorf("%s: %+v", tc.method, rpcErr)
		}
	}
	if raw, _ := os.ReadFile(s.cli.servers.Path); string(raw) != body {
		t.Errorf("bozuk dosya değişti: %s", raw)
	}
}

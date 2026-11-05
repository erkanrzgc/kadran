package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
)

const (
	hetznerTarget = "kadran-client@203.0.113.10"
	gcpTarget     = "kadran-client@198.51.100.7"
)

func savedList() []servers.Server {
	return []servers.Server{
		{Name: "hetzner", Target: hetznerTarget, Admin: "root@203.0.113.10"},
		{Name: "gcp", Target: gcpTarget, Admin: "ops@198.51.100.7", Sudo: true},
		{Name: "noadmin", Target: "kadran-client@192.0.2.5"},
	}
}

func loadSaved() ([]servers.Server, error) { return savedList(), nil }

func TestExpandServerFlag(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"status -s hetzner", "status " + hetznerTarget},
		{"deploy -s gcp blog", "deploy blog " + gcpTarget},
		{"app list --s=hetzner", "app list " + hetznerTarget},
		{"logs -s=gcp -tail 5 blog", "logs -tail 5 blog " + gcpTarget},
		{"bootstrap -s gcp", "bootstrap -sudo ops@198.51.100.7"},
		{"bootstrap -s hetzner", "bootstrap root@203.0.113.10"},
		{"bootstrap -s gcp -sudo=false", "bootstrap -sudo -sudo=false ops@198.51.100.7"},
		{"key list -s hetzner", "key list root@203.0.113.10"},
		{"key add -s gcp -deploy blog k.pub", "key add -sudo -deploy blog k.pub ops@198.51.100.7"},
		{"totp reset -s gcp", "totp reset -sudo ops@198.51.100.7"},
		{"totp status -s gcp", "totp status " + gcpTarget},
		{"status", "status"},
		{"status hetzner", "status hetzner"},
		{"status -- -s hetzner", "status -- -s hetzner"},
	} {
		got, name, err := expandServerFlag(strings.Fields(tc.in), loadSaved)
		if err != nil {
			t.Errorf("%q: %v", tc.in, err)
			continue
		}
		if strings.Join(got, " ") != tc.want {
			t.Errorf("%q → %q, beklenen %q", tc.in, strings.Join(got, " "), tc.want)
		}
		if strings.Contains(tc.in, "-s") && !strings.Contains(tc.in, "--") && name == "" {
			t.Errorf("%q: ad dönmedi", tc.in)
		}
	}
}

func TestExpandServerFlagErrors(t *testing.T) {
	for _, tc := range []struct {
		in    string
		usage bool
		want  string
	}{
		{"status -s hetzner -s gcp", true, "-s"},
		{"status -s", true, "-s"},
		{"version -s hetzner", true, "version"},
		{"server list -s hetzner", true, "server"},
		{"sidecar -s hetzner", true, "sidecar"},
		{"bootstrap -s noadmin", false, "-admin"},
		{"status -s Hetzner", false, "hetzner"},
		{"status -s hetz", false, "hetzner"},
	} {
		_, _, err := expandServerFlag(strings.Fields(tc.in), loadSaved)
		var sfe *serverFlagError
		if !errors.As(err, &sfe) {
			t.Errorf("%q: hata %v, *serverFlagError değil", tc.in, err)
			continue
		}
		if sfe.usage != tc.usage || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: usage=%v hata %q (beklenen usage=%v, %q içermeli)", tc.in, sfe.usage, err, tc.usage, tc.want)
		}
	}
}

// savedCLI, savedList'in kayıtlı olduğu geçici bir listesi olan cli.
func savedCLI(t *testing.T) (*cli, *bytes.Buffer) {
	t.Helper()
	c, _, errOut := newTestCLI("")
	c.servers = servers.Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}
	if _, err := c.servers.Update(func([]servers.Server) ([]servers.Server, error) { return savedList(), nil }); err != nil {
		t.Fatal(err)
	}
	return c, errOut
}

// flagCLI, savedCLI'a bağlanılan hedefleri kaydeden bir dial ekler.
func flagCLI(t *testing.T) (*cli, *[]string, *bytes.Buffer) {
	t.Helper()
	c, errOut := savedCLI(t)
	var dialed []string
	c.dial = func(_ context.Context, raw string) (*client.Client, *kadranv1.PingResponse, error) {
		dialed = append(dialed, raw)
		return nil, nil, errors.New("test: bağlantı yok")
	}
	return c, &dialed, errOut
}

func writeServersFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestServerFlagWithTargetNeverDials(t *testing.T) {
	for _, cmd := range []string{
		"status", "alarms", "app list", "app show blog", "app delete blog",
		"app update -domain a.example.com blog", "deploy blog", "rollback blog", "logs blog",
		"prune blog", "backup list", "backup create", "audit list", "audit verify",
		"domain check example.com", "login", "logout", "totp status",
	} {
		c, dialed, _ := flagCLI(t)
		// -s komutun hemen ardında, fazladan hedef sonda: kullanıcının yazacağı biçim.
		words := strings.Fields(cmd)
		head := 1
		if slices.Contains([]string{"app", "backup", "audit", "domain", "totp"}, words[0]) {
			head = 2
		}
		args := slices.Concat(words[:head], []string{"-s", "hetzner"}, words[head:], []string{"kadran-client@other"})
		code := c.run(t.Context(), args)
		if code != exitUsage {
			t.Errorf("%q: çıkış %d, beklenen kullanım hatası", strings.Join(args, " "), code)
		}
		if len(*dialed) != 0 {
			t.Errorf("%q: bağlanıldı: %v", strings.Join(args, " "), *dialed)
		}
	}
}

func TestServerFlagWithTargetSaysWhy(t *testing.T) {
	c, errOut := savedCLI(t)
	if code := c.run(t.Context(), []string{"status", "-s", "hetzner", "kadran-client@other"}); code != exitUsage {
		t.Fatalf("çıkış %d", code)
	}
	if !strings.Contains(errOut.String(), "-s hetzner already names the server") {
		t.Errorf("neden söylenmedi:\n%s", errOut)
	}
}

func TestServerFlagExactName(t *testing.T) {
	for _, name := range []string{"Hetzner", "hetz", "hetzner2"} {
		c, errOut := savedCLI(t)
		dialed := false
		c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
			dialed = true
			return nil, nil, errors.New("test")
		}
		if code := c.run(t.Context(), []string{"status", "-s", name}); code != exitError {
			t.Errorf("%q: çıkış %d", name, code)
		}
		if dialed {
			t.Errorf("%q: benzer ada bağlanıldı", name)
		}
		if !strings.Contains(errOut.String(), "hetzner, gcp, noadmin") {
			t.Errorf("%q: kayıtlı adlar söylenmedi:\n%s", name, errOut)
		}
	}
}

func TestServerFlagDialsSavedTarget(t *testing.T) {
	c, dialed, _ := flagCLI(t)
	c.run(t.Context(), []string{"status", "-s", "hetzner"})
	if !slices.Equal(*dialed, []string{hetznerTarget}) {
		t.Fatalf("bağlanılan %v, beklenen %s", *dialed, hetznerTarget)
	}
}

func TestServerFlagBadFileNeverDials(t *testing.T) {
	c, _, _ := newTestCLI("")
	c.servers = servers.Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}
	writeServersFile(t, c.servers.Path, `{"version":1,"servers":[{"name":"evil","target":"-oProxyCommand=calc@x"}]}`)
	dialed := false
	c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
		dialed = true
		return nil, nil, errors.New("test")
	}
	if code := c.run(t.Context(), []string{"status", "-s", "evil"}); code != exitError {
		t.Errorf("çıkış %d", code)
	}
	if dialed {
		t.Error("bozuk kayıtla bağlanıldı")
	}
}

// Uygulama adı unutulursa kayıtlı hedef ad yerine geçer ve hedef boş kalır;
// boş hedef yerel soket demek. connect bunu kayıtlı hedefle karşılaştırıp
// durdurmalı: hiçbir bağlantı denenmez.
func TestDeployWithoutAppNeverDialsElsewhere(t *testing.T) {
	for _, cmd := range [][]string{
		{"deploy", "-s", "hetzner"}, {"rollback", "-s", "hetzner"}, {"logs", "-s", "hetzner"},
		{"prune", "-s", "hetzner"}, {"app", "show", "-s", "hetzner"}, {"app", "delete", "-s", "hetzner"},
	} {
		c, dialed, errOut := flagCLI(t)
		if code := c.run(t.Context(), cmd); code == exitOK {
			t.Errorf("%v geçti", cmd)
		}
		if len(*dialed) != 0 {
			t.Errorf("%v: bağlanıldı: %q\n%s", cmd, *dialed, errOut)
		}
	}
}

func TestSessionHintUsesServerFlag(t *testing.T) {
	c, errOut := savedCLI(t)
	c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
		return nil, nil, status.Error(codes.Unauthenticated, `session expired: sign in again with "kadran login"`)
	}
	c.run(t.Context(), []string{"status", "-s", "hetzner"})
	if !strings.Contains(errOut.String(), "kadran login -s hetzner") {
		t.Errorf("ipucu -s kullanmıyor:\n%s", errOut)
	}
}

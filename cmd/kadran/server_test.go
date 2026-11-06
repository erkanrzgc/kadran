package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// serverCLI, kendi geçici sunucu listesi olan ve ağa çıkamayan bir cli.
func serverCLI(t *testing.T) (*cli, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c, out, errOut := newTestCLI("")
	c.servers = servers.Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}
	c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
		t.Fatal("`server` komutu ağa çıkmamalı")
		return nil, nil, nil
	}
	return c, out, errOut
}

func runOK(t *testing.T, c *cli, errOut *bytes.Buffer, args ...string) {
	t.Helper()
	errOut.Reset()
	if code := c.run(t.Context(), args); code != exitOK {
		t.Fatalf("%v: çıkış %d\n%s", args, code, errOut)
	}
}

func addTwo(t *testing.T, c *cli, errOut *bytes.Buffer) {
	t.Helper()
	runOK(t, c, errOut, "server", "add", "-provider", "Hetzner", "-admin", "root@203.0.113.10", "hetzner", "kadran-client@203.0.113.10")
	runOK(t, c, errOut, "server", "add", "-sudo", "-admin", "ops@198.51.100.7", "gcp", "kadran-client@198.51.100.7")
}

func TestServerAddAndList(t *testing.T) {
	c, out, errOut := serverCLI(t)
	addTwo(t, c, errOut)
	out.Reset()
	runOK(t, c, errOut, "server", "list")
	got := out.String()
	for _, want := range []string{"NAME", "PROVIDER", "TARGET", "ADMIN", "hetzner", "Hetzner", "kadran-client@203.0.113.10", "root@203.0.113.10", "ops@198.51.100.7 (sudo)"} {
		if !strings.Contains(got, want) {
			t.Errorf("liste %q içermiyor:\n%s", want, got)
		}
	}
	if strings.Index(got, "hetzner") > strings.Index(got, "gcp") {
		t.Errorf("ekleme sırası korunmadı:\n%s", got)
	}
}

func TestServerAddRefusesDuplicates(t *testing.T) {
	c, _, errOut := serverCLI(t)
	addTwo(t, c, errOut)
	for _, args := range [][]string{
		{"server", "add", "Hetzner", "kadran-client@192.0.2.1"},
		{"server", "add", "other", "kadran-client@203.0.113.10"},
	} {
		if code := c.run(t.Context(), args); code != exitError {
			t.Errorf("%v: çıkış %d, beklenen %d", args, code, exitError)
		}
	}
	list, err := c.servers.Load()
	if err != nil || len(list) != 2 {
		t.Fatalf("liste değişti: %+v, %v", list, err)
	}
}

func TestServerAddValidates(t *testing.T) {
	c, _, errOut := serverCLI(t)
	if code := c.run(t.Context(), []string{"server", "add", "evil", "-oProxyCommand=x@h"}); code == exitOK {
		t.Error("seçenek gibi hedef kaydedildi")
	}
	if code := c.run(t.Context(), []string{"server", "add", "-sudo", "a", "kadran-client@a"}); code != exitUsage {
		t.Errorf("-admin olmadan -sudo: çıkış %d\n%s", code, errOut)
	}
	for _, args := range [][]string{{"server", "add", "a"}, {"server", "add"}, {"server", "rm"}, {"server", "rename", "a"}, {"server"}, {"server", "edit"}} {
		if code := c.run(t.Context(), args); code != exitUsage {
			t.Errorf("%v: çıkış %d, beklenen kullanım hatası", args, code)
		}
	}
	if list, _ := c.servers.Load(); len(list) != 0 {
		t.Errorf("geçersiz kayıt yazıldı: %+v", list)
	}
}

func TestServerListEmpty(t *testing.T) {
	c, out, errOut := serverCLI(t)
	runOK(t, c, errOut, "server", "list")
	if !strings.Contains(out.String(), "kadran server add <name> <target>") {
		t.Errorf("boş liste nasıl ekleneceğini söylemiyor:\n%s", out)
	}
}

func TestServerRmAndRename(t *testing.T) {
	c, _, errOut := serverCLI(t)
	addTwo(t, c, errOut)
	runOK(t, c, errOut, "server", "rename", "gcp", "google")
	runOK(t, c, errOut, "server", "rm", "hetzner")
	list, err := c.servers.Load()
	if err != nil || len(list) != 1 {
		t.Fatalf("liste: %+v, %v", list, err)
	}
	if g := list[0]; g.Name != "google" || g.Target != "kadran-client@198.51.100.7" || g.Admin != "ops@198.51.100.7" || !g.Sudo {
		t.Errorf("rename alanları korumadı: %+v", g)
	}
	errOut.Reset()
	if code := c.run(t.Context(), []string{"server", "rm", "missing"}); code != exitError {
		t.Errorf("olmayan ad: çıkış %d", code)
	}
	if !strings.Contains(errOut.String(), "google") {
		t.Errorf("hata kayıtlı adları söylemiyor:\n%s", errOut)
	}
}

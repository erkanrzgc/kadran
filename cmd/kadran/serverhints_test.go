package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// listCLI, verilen listenin kayıtlı olduğu geçici bir listesi olan cli.
func listCLI(t *testing.T, list []servers.Server) (*cli, *bytes.Buffer) {
	t.Helper()
	c, _, errOut := newTestCLI("")
	c.servers = servers.Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}
	if _, err := c.servers.Update(func([]servers.Server) ([]servers.Server, error) { return list, nil }); err != nil {
		t.Fatal(err)
	}
	return c, errOut
}

// İpucundaki ad kopyalanıp bash, PowerShell ya da cmd'ye yapıştırılır: çift
// tırnak içinde üçünde de düz okunan karakterler tırnaklanır, tırnak içinde de
// yorumlananlar ($ ` " \ ! %) çalıştırılacak bir komuta hiç yazılmaz.
func TestShellName(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		ok       bool
	}{
		{"gcp", "gcp", true},
		{"hetzner-1.de", "hetzner-1.de", true},
		{"şirket", "şirket", true},
		{"a (2)", `"a (2)"`, true},
		{"Hetzner Almanya", `"Hetzner Almanya"`, true},
		{"2nd", `"2nd"`, true},
		{"a,b", `"a,b"`, true},
		{"@ops", `"@ops"`, true},
		{"a$b", `"a$b"`, false},
		{`a"b`, `"a\"b"`, false},
		{"a`b", "\"a`b\"", false},
		{`a\b`, `"a\\b"`, false},
		{"a!b", `"a!b"`, false},
		{"a%b%", `"a%b%"`, false},
	} {
		got, ok := shellName(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q → %q, %v; beklenen %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestNoAdminHintIsRunnable(t *testing.T) {
	load := func() ([]servers.Server, error) {
		return []servers.Server{{Name: "a (2)", Provider: "Hetzner Cloud", Target: "kadran-client@192.0.2.5"}}, nil
	}
	_, _, err := expandServerFlag([]string{"bootstrap", "-s", "a (2)"}, load)
	if err == nil {
		t.Fatal("yöneticisiz kayıtla bootstrap geçti")
	}
	msg := err.Error()
	for _, want := range []string{
		"\n  kadran server rm \"a (2)\"\n",
		"\n  kadran server add -provider \"Hetzner Cloud\" -admin root@host \"a (2)\" kadran-client@192.0.2.5\n",
		"-sudo",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("ipucunda %q yok:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "&&") {
		t.Errorf("&& PowerShell 5.1'de çalışmaz:\n%s", msg)
	}
}

func TestNoAdminHintSkipsUnsafeName(t *testing.T) {
	load := func() ([]servers.Server, error) {
		return []servers.Server{{Name: "a$b", Target: "kadran-client@192.0.2.5"}}, nil
	}
	_, _, err := expandServerFlag([]string{"bootstrap", "-s", "a$b"}, load)
	if err == nil {
		t.Fatal("yöneticisiz kayıtla bootstrap geçti")
	}
	if strings.Contains(err.Error(), "server rm a$b") || strings.Contains(err.Error(), `server rm "a$b"`) {
		t.Errorf("kabukta genişleyecek ad komuta yazıldı:\n%s", err)
	}
	if !strings.Contains(err.Error(), "-admin") {
		t.Errorf("ne yapılacağı söylenmedi:\n%s", err)
	}
}

func TestSessionHintQuotesName(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"a (2)", `kadran login -s "a (2)"`},
		{"a$b", "kadran login kadran-client@192.0.2.9"},
	} {
		c, errOut := listCLI(t, []servers.Server{{Name: tc.name, Target: "kadran-client@192.0.2.9"}})
		c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
			return nil, nil, status.Error(codes.Unauthenticated, `session expired: sign in again with "kadran login"`)
		}
		c.run(t.Context(), []string{"status", "-s", tc.name})
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%q: ipucu %q değil:\n%s", tc.name, tc.want, errOut)
		}
	}
}

// "-s zaten sunucuyu söylüyor" notu yalnız argüman sayısı hatasında: başka
// bir kullanım hatasında (bilinmeyen alt komut, geçersiz bayrak) yanıltır.
func TestServerNoteOnlyOnCountErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		note bool
	}{
		{[]string{"status", "-s", "hetzner", "kadran-client@other"}, true},
		{[]string{"deploy", "-s", "hetzner", "blog", "kadran-client@other"}, true},
		{[]string{"key", "bogus", "-s", "hetzner"}, false},
		{[]string{"domain", "check", "-s", "hetzner", "-wait=-1s", "example.com"}, false},
		{[]string{"key", "remove", "-s", "hetzner", "nofp"}, false},
	} {
		c, errOut := savedCLI(t)
		if code := c.run(t.Context(), tc.args); code != exitUsage {
			t.Errorf("%v: çıkış %d, beklenen kullanım hatası", tc.args, code)
		}
		if got := strings.Contains(errOut.String(), "already names the server"); got != tc.note {
			t.Errorf("%v: not %v, beklenen %v:\n%s", tc.args, got, tc.note, errOut)
		}
	}
}

func TestCommandHelpListsServerFlag(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want bool
	}{
		{[]string{"status", "-h"}, true},
		{[]string{"deploy", "-h"}, true},
		{[]string{"key", "add", "-h"}, true},
		{[]string{"server", "add", "-h"}, false},
		{[]string{"sidecar", "-h"}, false},
	} {
		c, _, errOut := newTestCLI("")
		c.run(t.Context(), tc.args)
		if got := strings.Contains(errOut.String(), "-s name"); got != tc.want {
			t.Errorf("%v: -s satırı %v, beklenen %v:\n%s", tc.args, got, tc.want, errOut)
		}
	}
}

func TestVerifyHint(t *testing.T) {
	for _, tc := range []struct{ name, host, want string }{
		{"", "203.0.113.10", "kadran status kadran-client@203.0.113.10"},
		{"gcp", "198.51.100.7", "kadran status -s gcp"},
		{"a (2)", "198.51.100.7", `kadran status -s "a (2)"`},
		{"a$b", "198.51.100.7", "kadran status kadran-client@198.51.100.7"},
	} {
		if got := verifyHint(tc.name, tc.host); got != tc.want {
			t.Errorf("verifyHint(%q, %q) = %q, beklenen %q", tc.name, tc.host, got, tc.want)
		}
	}
}

// `domain check -s ad` alan adı unutulursa kayıtlı hedef alan adının yerine
// geçer; komut connect'ten geçmediği için o sunucu HTTP'den yoklanırdı.
func TestDomainCheckWithoutDomainNeverProbes(t *testing.T) {
	c, errOut := savedCLI(t)
	out := &bytes.Buffer{}
	c.stdout = out
	c.resolver = cliResolver{}
	probed := false
	c.dialNet = func(context.Context, string, string) (net.Conn, error) {
		probed = true
		return nil, errors.New("test: ağ yok")
	}
	c.sshHostname = func(context.Context, string) (string, error) {
		probed = true
		return "", errors.New("test: ağ yok")
	}
	if code := c.run(t.Context(), []string{"domain", "check", "-s", "hetzner"}); code == exitOK {
		t.Fatal("alan adsız domain check geçti")
	}
	if probed || strings.Contains(out.String(), "Domain:") {
		t.Errorf("kayıtlı hedef alan adı diye yoklandı:\n%s", out)
	}
	if !strings.Contains(errOut.String(), "-s hetzner") {
		t.Errorf("neden söylenmedi:\n%s", errOut)
	}
}

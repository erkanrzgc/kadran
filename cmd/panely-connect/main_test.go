package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/erkanrzgc/kadran/internal/connproto"
)

func TestNoFlagMeansAdmin(t *testing.T) {
	opts, err := parseArgs(nil, io.Discard)
	if err != nil {
		t.Fatalf("argümansız çağrı reddedildi: %v", err)
	}
	if opts.role != connproto.RoleAdmin || len(opts.apps) != 0 {
		t.Fatalf("rol = %q, kapsam = %q; yönetici bekleniyordu — mevcut her authorized_keys satırı argümansız",
			opts.role, opts.apps)
	}
	if opts.socket != defaultSocket {
		t.Errorf("soket = %q, beklenen %q", opts.socket, defaultSocket)
	}
}

func TestDeployFlagSetsScope(t *testing.T) {
	for _, args := range [][]string{
		{"-deploy=web,api"},
		{"-deploy", "web,api"},
	} {
		opts, err := parseArgs(args, io.Discard)
		if err != nil {
			t.Fatalf("%q reddedildi: %v", args, err)
		}
		if opts.role != connproto.RoleDeploy || !reflect.DeepEqual(opts.apps, []string{"web", "api"}) {
			t.Errorf("%q: rol = %q, kapsam = %q", args, opts.role, opts.apps)
		}
	}
}

// TestBadArgumentsAreRejected: her ret, bağlantının hiç kurulmaması
// demek. Yanlış yazılmış bir dağıtım satırı yöneticiye DÜŞMEMELİ.
func TestBadArgumentsAreRejected(t *testing.T) {
	for _, args := range [][]string{
		{"-deploy="},                   // boş kapsam
		{"-deploy=web", "-deploy=api"}, // iki kez: hangisi geçerli belirsiz
		{"-deploy=web,"},               // boş öğe
		{"-deploy=$(id)"},              // kabuk
		{"-deploy=*"},                  // joker yok
		{"fazla"},                      // konumsal argüman
		{"-deploy=web", "fazla"},       // bayraktan sonra konumsal
		{"-bilinmeyen"},                // bilinmeyen bayrak
	} {
		if opts, err := parseArgs(args, io.Discard); err == nil {
			t.Errorf("%q kabul edildi: %+v", args, opts)
		}
	}
}

func TestIdentityCarriesRole(t *testing.T) {
	// sshd'nin ExposeAuthInfo ile yaptığı: dosya + SSH_USER_AUTH (K-134).
	auth := filepath.Join(t.TempDir(), "sshauth")
	if err := os.WriteFile(auth, []byte("publickey ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := map[string]string{
		"SSH_CONNECTION": "203.0.113.7 51000 198.51.100.1 22",
		"SSH_USER_AUTH":  auth,
	}
	opts := options{role: connproto.RoleDeploy, apps: []string{"web"}}

	id := resolveIdentity(func(k string) string { return env[k] }, opts)
	if id.Origin != "ssh" || id.Fingerprint == "" || id.SourceIP != "203.0.113.7" {
		t.Fatalf("SSH kimliği eksik: %+v", id)
	}
	if id.Role != connproto.RoleDeploy || !reflect.DeepEqual(id.Apps, []string{"web"}) {
		t.Fatalf("rol önsöze geçmedi: %+v", id)
	}

	local := resolveIdentity(func(string) string { return "" }, opts)
	if local.Origin != "local" || local.Role != connproto.RoleDeploy {
		t.Fatalf("yerel çağrıda rol kayboldu: %+v", local)
	}
	if err := id.CheckRole(); err != nil {
		t.Fatalf("üretilen kimlik panelyd'nin denetiminden geçmiyor: %v", err)
	}
}

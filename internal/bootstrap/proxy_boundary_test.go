package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBootstrapCarriesProxyBroker(t *testing.T) {
	repo := newFakeRepo(t)
	archive, err := buildArchive(Options{BinaryDir: filepath.Join(repo, "bin"), RepoRoot: repo, ClientKeyPath: filepath.Join(repo, "key.pub")}, "amd64")
	if err != nil {
		t.Fatal(err)
	}
	files := readArchive(t, archive)
	for _, f := range []string{"kadran-proxy", "kadran-proxy.service", "kadran-proxy.socket", "kadran-proxy-tmpfiles.conf"} {
		if _, ok := files[f]; !ok {
			t.Errorf("broker artifact missing: %s", f)
		}
	}
}

func TestRawCaddySocketExcludesDaemonGroup(t *testing.T) {
	b, err := os.ReadFile("../../deploy/systemd/kadran-caddy-admin.socket")
	if err != nil {
		t.Fatal(err)
	}
	directives := strings.Join(unitDirectives(string(b)), "\n")
	if !strings.Contains(directives, "SocketGroup=kadran-proxy\n") {
		t.Error("raw Caddy admin socket is not owned by broker group")
	}
	if strings.Contains(directives, "SocketGroup=kadran\n") {
		t.Error("daemon still controls raw Caddy admin")
	}
}

func TestBrokerUnitsEnforceTheSeparateIdentity(t *testing.T) {
	socket := readUnit(t, "kadran-proxy.socket")
	for key, want := range map[string]string{"ListenStream": "/run/kadran-proxy/proxy.sock", "SocketUser": "kadran-proxy", "SocketGroup": "kadran", "SocketMode": "0660", "RemoveOnStop": "yes", "Service": "kadran-proxy.service"} {
		got := directive(socket, key)
		if len(got) != 1 || got[0] != want {
			t.Errorf("typed socket %s=%v, want %s", key, got, want)
		}
	}
	unit := readUnit(t, "kadran-proxy.service")
	for key, want := range map[string]string{"User": "kadran-proxy", "Group": "kadran-proxy", "RestrictAddressFamilies": "AF_UNIX", "CapabilityBoundingSet": "", "AmbientCapabilities": ""} {
		got := directive(unit, key)
		if len(got) != 1 || got[0] != want {
			t.Errorf("broker %s=%v, want %s", key, got, want)
		}
	}
	if groups := directive(unit, "SupplementaryGroups"); len(groups) != 0 {
		t.Errorf("broker gains supplementary groups: %v", groups)
	}
	inaccessible := strings.Join(directive(unit, "InaccessiblePaths"), " ")
	for _, path := range []string{"/run/kadran-exec", "/run/docker.sock", "/var/lib/kadran-caddy"} {
		if !strings.Contains(inaccessible, path) {
			t.Errorf("broker can see %s", path)
		}
	}
	daemon := strings.Join(directive(readUnit(t, "kadrand.service"), "InaccessiblePaths"), " ")
	if !strings.Contains(daemon, "/run/kadran-caddy") {
		t.Error("daemon can still see raw admin directory")
	}
}

func TestUpgradeClosesLegacyRawAdminConnections(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	block := between(string(script), "ters_vekil_baslat() {", "ters_vekil_kur() {")
	stop := strings.Index(block, "systemctl stop kadrand.service")
	restart := strings.Index(block, "systemctl restart kadran-caddy-admin.socket")
	if stop < 0 || restart < 0 || stop >= restart {
		t.Error("upgrade does not stop legacy daemon before reopening raw admin")
	}
	if !strings.Contains(block, "systemctl stop kadran-caddy.service") {
		t.Error("old raw admin accepted connections survive upgrade")
	}
	for _, name := range []string{"kadran-proxy.service", "kadran-proxy.socket"} {
		if !strings.Contains(string(script), "systemctl enable "+name) {
			t.Errorf("broker unit %s not enabled", name)
		}
	}
}

// kadrand.service Requires=kadran-proxy.socket. Göçte ters_vekil_*
// kadrand'dan sonraya ertelendiği için aracı soketi kadrand'ın ilk yeniden
// başlatmasından ÖNCE ayrıca hazırlanmalı (B2/I4).
func TestMigrationPreparesBrokerSocketBeforeRestartingDaemon(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)
	call := strings.Index(text, "\n    vekil_araci_onhazirla\n")
	restart := strings.Index(text, "\nsystemctl restart kadrand.service\n")
	if call < 0 || restart < 0 || call >= restart {
		t.Fatalf("broker preparation must precede the daemon restart (call=%d restart=%d)", call, restart)
	}
	fn := between(text, "vekil_araci_onhazirla() {", "\n}\n")
	if strings.Contains(fn, "systemctl start kadran-proxy.service") {
		t.Error("broker service must not start before the old proxy releases the ports")
	}
}

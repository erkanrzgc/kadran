//go:build linux

package proxyguard

import (
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// A synthetic local permission fixture uses the deployed unit identities and
// modes without creating production accounts, touching /run or running Caddy.
func TestUnixPermissionsSeparateRawTypedAndExecutorSockets(t *testing.T) {
	if os.Geteuid() != 0 {
		if os.Getenv("KADRAN_TEST_PROXY_PERMISSIONS") == "1" {
			t.Fatal("proxy permission test requires root")
		}
		t.Skip("synthetic numeric UID/GID fixture requires root")
	}
	base, err := os.MkdirTemp("/tmp", "kadran-proxy-permissions-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(base) }()
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(base, "helper")
	if err := os.WriteFile(helper, contents, 0755); err != nil {
		t.Fatal(err)
	}
	const caddyID = 65531
	const brokerID = 65532
	const daemonID = 65533
	makeSocket := func(name string, owner, group int) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, 0750); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(dir, owner, group); err != nil {
			t.Fatal(err)
		}
		sock := filepath.Join(dir, "socket")
		ln, err := net.Listen("unix", sock)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = ln.Close() })
		if err := os.Chown(sock, owner, group); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(sock, 0660); err != nil {
			t.Fatal(err)
		}
		return sock
	}
	raw := makeSocket("raw", caddyID, brokerID)
	typed := makeSocket("typed", brokerID, daemonID)
	executor := makeSocket("exec", 0, daemonID)
	connect := func(uid uint32, path string) error {
		cmd := exec.CommandContext(t.Context(), helper, "-test.run=^TestProxyConnectHelper$")
		cmd.Env = append(os.Environ(), "KADRAN_TEST_PROXY_SOCKET="+path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: uid}}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("uid %d socket %s: %s", uid, path, out)
		}
		return err
	}
	if err := connect(brokerID, raw); err != nil {
		t.Fatal("broker cannot reach raw admin", err)
	}
	if err := connect(daemonID, typed); err != nil {
		t.Fatal("daemon cannot reach typed socket", err)
	}
	if err := connect(daemonID, raw); err == nil {
		t.Fatal("daemon reached raw admin")
	}
	if err := connect(brokerID, executor); err == nil {
		t.Fatal("broker reached privileged executor")
	}
	if err := connect(caddyID, executor); err == nil {
		t.Fatal("Caddy reached privileged executor")
	}
	// Simulate the old install: granting daemon group access must break the test.
	if err := os.Chown(filepath.Dir(raw), caddyID, daemonID); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(raw, caddyID, daemonID); err != nil {
		t.Fatal(err)
	}
	if err := connect(daemonID, raw); err != nil {
		t.Fatal("positive control: legacy daemon raw access was not measured", err)
	}
}

func TestProxyConnectHelper(t *testing.T) {
	socket := os.Getenv("KADRAN_TEST_PROXY_SOCKET")
	if socket == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os/exec"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
	"github.com/erkanrzgc/kadran/internal/version"
)

// envTotpRun, totp enable'ı Env kancalarıyla koşar; oturum ve sunucu
// listesi geçici dizinde (gerçek yapılandırma dizinine yazılmasın).
func envTotpRun(t *testing.T, env Env) int {
	t.Helper()
	rpc := &fakeTotpRPC{}
	env.Stdin = strings.NewReader("123456\n")
	env.Dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
		return client.NewWithRPC(rpc), &kadranv1.PingResponse{}, nil
	}
	c := newCLI(env)
	c.sessions = client.SessionStore{Dir: t.TempDir()}
	c.servers = servers.Store{Path: t.TempDir() + "/servers.json"}
	return c.run(context.Background(), []string{"totp", "enable", totpHedef})
}

func TestSensitiveGetsEnrollmentOnly(t *testing.T) {
	var out, errOut, secret bytes.Buffer
	if code := envTotpRun(t, Env{Stdout: &out, Stderr: &errOut, Sensitive: &secret}); code != ExitOK {
		t.Fatalf("çıkış %d: %s", code, &errOut)
	}
	q, err := qrBlock("otpauth://totp/Kadran:srv?issuer=Kadran&secret=JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"JBSWY3DPEHPK3PXP", fakeOtpauthURI, q, "AAAAA-BBBBB", "CCCCC-DDDDD",
		"recovery codes (shown once, store them offline):"} {
		if !strings.Contains(secret.String(), s) {
			t.Errorf("Sensitive'te %q yok:\n%s", s, &secret)
		}
		if strings.Contains(out.String(), s) || strings.Contains(errOut.String(), s) {
			t.Errorf("%q Stdout/Stderr'e sızdı", s)
		}
	}
	for _, s := range []string{"TOTP is on.", "signed in until "} {
		if !strings.Contains(out.String(), s) {
			t.Errorf("Stdout'ta %q yok:\n%s", s, &out)
		}
		if strings.Contains(secret.String(), s) {
			t.Errorf("%q Sensitive'e gitti", s)
		}
	}
}

func TestSensitiveNilKeepsStdout(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := envTotpRun(t, Env{Stdout: &out, Stderr: &errOut}); code != ExitOK {
		t.Fatalf("çıkış %d: %s", code, &errOut)
	}
	if got := zamansiz(out.String()); got != totpEnableGolden {
		t.Fatalf("stdout taşıma öncesinden farklı:\n%q\nbeklenen:\n%q", got, totpEnableGolden)
	}
}

// sshKancasi, çağrıları kaydeden ve çalıştırılamayan bir komut döndüren
// Command kancası: ağa çıkılmaz, komut hatayla biter.
type sshKancasi struct{ names []string }

func (k *sshKancasi) command(_ context.Context, name string, _ ...string) *exec.Cmd {
	k.names = append(k.names, name)
	return exec.Command("kadran-yok-boyle-bir-program")
}

func (k *sshKancasi) sawSSH() bool {
	for _, n := range k.names {
		if n == "ssh" {
			return true
		}
	}
	return false
}

func TestCommandHookSeesSSH(t *testing.T) {
	t.Run("domain check", func(t *testing.T) {
		hook := &sshKancasi{}
		c := newCLI(Env{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{}, Command: hook.command})
		c.resolver = cliResolver{"ip4 example.com": {"203.0.113.10"}}
		c.dialNet = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("ağ yok") }
		c.servers = servers.Store{Path: t.TempDir() + "/servers.json"}
		c.run(context.Background(), []string{"domain", "check", "example.com", "kimse@203.0.113.10"})
		if !hook.sawSSH() {
			t.Fatalf("ssh -G kancadan geçmedi: %v", hook.names)
		}
	})
	t.Run("key list", func(t *testing.T) {
		hook := &sshKancasi{}
		var errOut bytes.Buffer
		c := newCLI(Env{Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &errOut, Command: hook.command})
		c.servers = servers.Store{Path: t.TempDir() + "/servers.json"}
		code := c.run(context.Background(), []string{"key", "list", "root@203.0.113.10"})
		if code != ExitError {
			t.Fatalf("çıkış %d, beklenen %d: %s", code, ExitError, &errOut)
		}
		if !hook.sawSSH() {
			t.Fatalf("anahtar listesinin ssh'ı kancadan geçmedi: %v", hook.names)
		}
	})
}

func TestGetenvHook(t *testing.T) {
	old := version.Version
	version.Version = "9.9.9"
	t.Cleanup(func() { version.Version = old })

	asked := map[string]bool{}
	hook := &sshKancasi{}
	c := newCLI(Env{
		Stdin: strings.NewReader(""), Stdout: &bytes.Buffer{}, Stderr: &bytes.Buffer{},
		Command: hook.command,
		Getenv: func(k string) string {
			asked[k] = true
			if k == "KADRAN_RELEASE_BASE" {
				return "http://127.0.0.1:1"
			}
			return ""
		},
		UserHomeDir: func() (string, error) { return t.TempDir(), nil },
	})
	c.servers = servers.Store{Path: t.TempDir() + "/servers.json"}
	c.run(context.Background(), []string{"bootstrap", "root@203.0.113.10"})
	if !asked["KADRAN_RELEASE_BASE"] || !asked["KADRAN_BINARY_DIR"] {
		t.Fatalf("Getenv kancası sürüm yolunda çağrılmadı: %v", asked)
	}
}

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/bootstrap"
)

// sahteAnahtarlar, sunucunun authorized_keys'ini taklit eder; satırlar
// GERÇEK ayrıştırıcıdan geçiyor.
type sahteAnahtarlar struct {
	icerik string
	opts   bootstrap.KeyOptions
	pub    []byte
	apps   []string
	name   string
	fp     string
	hata   error
}

func (s *sahteAnahtarlar) List(_ context.Context, o bootstrap.KeyOptions) ([]bootstrap.AuthorizedKey, error) {
	s.opts = o
	return bootstrap.ParseAuthorizedKeys(s.icerik), s.hata
}

func (s *sahteAnahtarlar) AddDeploy(_ context.Context, o bootstrap.KeyOptions, pub []byte, apps []string, name string) (bootstrap.AuthorizedKey, error) {
	s.opts, s.pub, s.apps, s.name = o, pub, apps, name
	if s.hata != nil {
		return bootstrap.AuthorizedKey{}, s.hata
	}
	return bootstrap.AuthorizedKey{Role: "deploy", Apps: apps, Fingerprint: "SHA256:yeni"}, nil
}

func (s *sahteAnahtarlar) Remove(_ context.Context, o bootstrap.KeyOptions, fp string) (bootstrap.AuthorizedKey, error) {
	s.opts, s.fp = o, fp
	return bootstrap.AuthorizedKey{Role: "deploy", Fingerprint: fp}, s.hata
}

// Gerçek ed25519 gövdeleri: ayrıştırıcı parmak izini gövdeden hesaplıyor.
const (
	yoneticiGovde = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl"
	ciGovde       = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHgu0dsIZuKyH1qgSeTE0FTeB0ZXcGtGmPdBQjfHC5BD"
)

func anahtarCLI(t *testing.T, s *sahteAnahtarlar) (*cli, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	c, out, errOut := newTestCLI("")
	c.keys = s
	return c, out, errOut
}

func TestKeyListShowsRolesAndScopes(t *testing.T) {
	s := &sahteAnahtarlar{icerik: strings.Join([]string{
		`command="/usr/local/lib/panely/panely-connect",restrict ` + yoneticiGovde + " erkan@dizustu",
		`command="/usr/local/lib/panely/panely-connect -deploy=site,api",restrict ` + ciGovde + " ci",
	}, "\n")}
	c, out, _ := anahtarCLI(t, s)

	if code := c.run(context.Background(), []string{"key", "list", "-sudo", "erkan@sunucu:2222"}); code != exitOK {
		t.Fatalf("çıkış kodu %d", code)
	}
	o := out.String()
	for _, want := range []string{"yönetici", "dağıtım", "site,api", "erkan@dizustu", "SHA256:"} {
		if !strings.Contains(o, want) {
			t.Errorf("çıktıda %q yok:\n%s", want, o)
		}
	}
	if s.opts != (bootstrap.KeyOptions{Host: "erkan@sunucu", Port: 2222, Sudo: true}) {
		t.Errorf("hedef yanlış iletildi: %+v", s.opts)
	}
}

// TestKeyListFlagsUnrestrictedLines: kısıtsız satır panely-client'a kabuk
// açar. Liste bunu GÖSTERMELİ ve çıkış koduyla bildirmeli.
func TestKeyListFlagsUnrestrictedLines(t *testing.T) {
	s := &sahteAnahtarlar{icerik: strings.Join([]string{
		`command="/usr/local/lib/panely/panely-connect",restrict ` + yoneticiGovde + " erkan",
		ciGovde + " elle-eklenmis",
	}, "\n")}
	c, out, errOut := anahtarCLI(t, s)

	if code := c.run(context.Background(), []string{"key", "list", "root@sunucu"}); code != exitError {
		t.Fatalf("kısıtsız satıra rağmen çıkış kodu %d", code)
	}
	if !strings.Contains(out.String(), "KISITSIZ") {
		t.Errorf("kısıtsız satır işaretlenmedi:\n%s", out.String())
	}
	if !strings.Contains(errOut.String(), "kabuk") {
		t.Errorf("uyarı nedenini söylemiyor:\n%s", errOut.String())
	}
}

func TestKeyAddPassesScopeAndKey(t *testing.T) {
	s := &sahteAnahtarlar{}
	c, out, _ := anahtarCLI(t, s)
	pub := filepath.Join(t.TempDir(), "ci.pub")
	if err := os.WriteFile(pub, []byte(ciGovde+" ci\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code := c.run(context.Background(), []string{"key", "add", "-deploy", "site,api", "-name", "gh", pub, "root@sunucu"})
	if code != exitOK {
		t.Fatalf("çıkış kodu %d", code)
	}
	if !reflect.DeepEqual(s.apps, []string{"site", "api"}) || s.name != "gh" || string(s.pub) != ciGovde+" ci\n" {
		t.Fatalf("iletilen: kapsam %q, ad %q, anahtar %q", s.apps, s.name, s.pub)
	}
	// CI'da nasıl kullanılacağını söylemeli: -commit şart (K-131).
	if !strings.Contains(out.String(), "-commit") {
		t.Errorf("kullanım ipucu yok:\n%s", out.String())
	}
}

func TestKeyUsageErrors(t *testing.T) {
	pub := filepath.Join(t.TempDir(), "ci.pub")
	if err := os.WriteFile(pub, []byte(ciGovde), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"key"},
		{"key", "bilinmeyen"},
		{"key", "list"},                    // hedef yok
		{"key", "add", pub, "root@sunucu"}, // -deploy yok
		{"key", "add", "-deploy", "site", "root@sunucu"}, // anahtar yok
		{"key", "add", "-deploy", "Site", pub, "root@s"}, // geçersiz kapsam
		{"key", "remove", "root@sunucu"},                 // parmak izi yok
		{"key", "remove", "MD5:ab", "root@sunucu"},       // biçim
		{"key", "list", "/run/panely/api.sock"},          // yerel soket
	} {
		s := &sahteAnahtarlar{}
		c, _, _ := anahtarCLI(t, s)
		if code := c.run(context.Background(), args); code == exitOK {
			t.Errorf("%q başarılı döndü", args)
		}
		if s.opts != (bootstrap.KeyOptions{}) {
			t.Errorf("%q: geçersiz çağrı sunucuya gitti: %+v", args, s.opts)
		}
	}
}

// TestKeyAddRequiresDeployScope: yönetici anahtarı bu komutla eklenmiyor;
// -deploy unutulursa hata NEDENİNİ söylemeli, kapsam ayrıştırıcısının
// genel "boş kapsam" mesajını değil.
func TestKeyAddRequiresDeployScope(t *testing.T) {
	pub := filepath.Join(t.TempDir(), "ci.pub")
	if err := os.WriteFile(pub, []byte(ciGovde), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _, errOut := anahtarCLI(t, &sahteAnahtarlar{})
	if code := c.run(context.Background(), []string{"key", "add", pub, "root@sunucu"}); code != exitUsage {
		t.Fatalf("çıkış kodu %d", code)
	}
	if !strings.Contains(errOut.String(), "-deploy zorunlu") {
		t.Fatalf("hata nedeni söylenmiyor: %q", errOut.String())
	}
}

func TestKeyRemoveReportsServerRefusal(t *testing.T) {
	s := &sahteAnahtarlar{hata: errors.New("son yönetici anahtarı kaldırılamaz")}
	c, _, errOut := anahtarCLI(t, s)
	code := c.run(context.Background(), []string{"key", "remove", "SHA256:abc", "root@sunucu"})
	if code != exitError || !strings.Contains(errOut.String(), "son yönetici") {
		t.Fatalf("kod %d, çıktı %q", code, errOut.String())
	}
	if s.fp != "SHA256:abc" {
		t.Errorf("parmak izi iletilmedi: %q", s.fp)
	}
}

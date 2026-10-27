package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// ── Kökün kaçış yolu: uzak komut (K-155) ──────────────────────────────

// TestResetTOTPRemoteCommand: sıfırlama, anahtar işlemleriyle aynı yetki
// yolundan (root'a SSH ya da sudo -n) sabit betiği çağırıyor ve sunucunun
// çıktısını aktarıyor.
func TestResetTOTPRemoteCommand(t *testing.T) {
	for _, tc := range []struct {
		opts KeyOptions
		want string
	}{
		{KeyOptions{Host: "root@srv.example"}, "totp-reset"},
		{KeyOptions{Host: "erkan@srv.example", Port: 2222, Sudo: true}, "totp-reset sudo"},
	} {
		log := sahteSSH(t, nil)
		var out bytes.Buffer
		if err := ResetTOTP(context.Background(), tc.opts, &out); err != nil {
			t.Fatalf("%+v: %v", tc.opts, err)
		}
		if got := cagrilar(t, log); len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%+v: çağrılar %v, beklenen [%s]", tc.opts, got, tc.want)
		}
		if !strings.Contains(out.String(), "TOTP disabled; every session is invalid") {
			t.Fatalf("sunucunun çıktısı aktarılmadı: %q", out.String())
		}
	}
}

// TestResetTOTPScriptDropsToKadran: betik kadrand'yi root olarak DEĞİL
// kadran kullanıcısı olarak çalıştırıyor (kadrand root'u reddeder ve
// veritabanı onun); ortam temizleniyor.
func TestResetTOTPScriptDropsToKadran(t *testing.T) {
	for _, want := range []string{
		"exec setpriv --reuid kadran --regid kadran --clear-groups -- \\\n",
		"env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin /usr/local/lib/kadran/kadrand -totp-reset",
	} {
		if !strings.Contains(remoteTOTPReset, want) {
			t.Fatalf("betikte %q yok:\n%s", want, remoteTOTPReset)
		}
	}
}

// TestRemoteTOTPResetRealBash: betik gerçek bash'te ayrışıyor ve kadran
// kullanıcısı olmayan (kurulmamış) bir makinede setpriv'e varmadan 3 ile
// ve sebebini söyleyerek çıkıyor. Sahte ssh betiği yalnız tanıyor, koşmuyor.
func TestRemoteTOTPResetRealBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		if runtime.GOOS == "linux" {
			t.Fatalf("bash Linux'ta bulunamadı: %v", err)
		}
		t.Skipf("bash yok: %v", err)
	}
	if out, err := exec.Command(bash, "-n", "-c", remoteTOTPReset).CombinedOutput(); err != nil {
		t.Fatalf("betik ayrışmıyor: %v\n%s", err, out)
	}
	if exec.Command(bash, "-c", "id -u kadran").Run() == nil {
		t.Skip("bu makinede kadran kullanıcısı var; kurulmamış sunucu durumu sınanamaz")
	}
	cmd := exec.Command(bash, "-c", remoteTOTPReset)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != totpResetNotInstalled {
		t.Fatalf("çıkış %v, beklenen %d (stderr %q)", err, totpResetNotInstalled, stderr.String())
	}
	if !strings.Contains(stderr.String(), "run kadran bootstrap first") {
		t.Fatalf("sebep söylenmedi: %q", stderr.String())
	}
}

func TestResetTOTPErrors(t *testing.T) {
	cases := []struct {
		name string
		opts KeyOptions
		rc   string
		want string
	}{
		{"sudo parola istedi", KeyOptions{Host: "erkan@srv.example", Sudo: true}, "1",
			"totp reset failed (code 1; -sudo NEVER asks for a password, NOPASSWD is needed): sunucu dedi ki"},
		{"bağlantı yok", KeyOptions{Host: "root@srv.example"}, "255", "could not connect to the server: sunucu dedi ki"},
		{"kurulu değil", KeyOptions{Host: "root@srv.example"}, "3", "sunucu dedi ki"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sahteSSH(t, map[string]string{fakeTOTPResetRCEnv: tc.rc})
			err := ResetTOTP(context.Background(), tc.opts, &bytes.Buffer{})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("hata %v, beklenen %q", err, tc.want)
			}
		})
	}
}

func TestResetTOTPRejectsOptionLikeHost(t *testing.T) {
	log := sahteSSH(t, nil)
	if err := ResetTOTP(context.Background(), KeyOptions{Host: "-oProxyCommand=x"}, &bytes.Buffer{}); err == nil {
		t.Fatal("seçenek gibi görünen hedef kabul edildi")
	}
	if got := cagrilar(t, log); len(got) != 0 {
		t.Fatalf("ssh çağrıldı: %v", got)
	}
}

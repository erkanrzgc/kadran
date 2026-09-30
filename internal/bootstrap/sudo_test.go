package bootstrap

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ── Sudo kipi (K-122) ────────────────────────────────────────────────
//
// Bulut imajlarının çoğu root'a SSH'ı kapalı getiriyor (GCP Debian:
// `PermitRootLogin no`, K-121). Sudo kipi kurulumu, sshd politikasına
// dokunmadan, hedef kullanıcının PAROLASIZ sudo'suyla yapıyor.
//
// Buradaki sahte ssh yalnızca argv'nin BİÇİMİNİ ve çağrı SIRASINI
// kanıtlar; gerçek sudo'yu, stdin'in sudo'dan geçmesini ve uzaktaki
// kabuğun tırnakları nasıl çözdüğünü KANITLAMAZ. Onlar gerçek sunucuda
// ölçülür (K-122).

const (
	fakeSSHEnv      = "PANELY_TEST_FAKE_SSH"
	fakeSSHLogEnv   = "PANELY_TEST_FAKE_SSH_LOG"
	fakeSSHUIDEnv   = "PANELY_TEST_FAKE_SSH_UID"
	fakeSudoFailEnv = "PANELY_TEST_FAKE_SUDO_FAIL"
	callSeparator   = "\n--- çağrı ---\n"
)

func TestMain(m *testing.M) {
	if os.Getenv(fakeSSHEnv) != "" {
		fakeSSHMain()
		return
	}
	os.Exit(m.Run())
}

// fakeSSHMain, uzak komuta (argv'nin sonu) göre sunucuyu taklit eder.
func fakeSSHMain() {
	remote := os.Args[len(os.Args)-1]
	if f, err := os.OpenFile(os.Getenv(fakeSSHLogEnv), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
		_, _ = f.WriteString(remote + callSeparator)
		_ = f.Close()
	}
	switch {
	case remote == "uname -m":
		_, _ = os.Stdout.WriteString("x86_64\n")
	case strings.HasSuffix(remote, "id -u") || strings.HasSuffix(remote, "'id -u'"):
		if strings.HasPrefix(remote, "sudo ") {
			if msg := os.Getenv(fakeSudoFailEnv); msg != "" {
				_, _ = os.Stderr.WriteString(msg + "\n")
				os.Exit(1)
			}
			_, _ = os.Stdout.WriteString("0\n")
			return
		}
		uid := os.Getenv(fakeSSHUIDEnv)
		if uid == "" {
			uid = "0"
		}
		_, _ = os.Stdout.WriteString(uid + "\n")
	default:
		// Kurulum: paketi tüket.
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
}

// sahteSSH, sshCommand'ı test binary'sine çevirir; çağrı günlüğünün yolunu döndürür.
func sahteSSH(t *testing.T, env map[string]string) string {
	t.Helper()
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}
	log := filepath.Join(t.TempDir(), "ssh.log")
	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHLogEnv, log)
	for k, v := range env {
		t.Setenv(k, v)
	}
	original := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = original })
	return log
}

func cagrilar(t *testing.T, log string) []string {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("çağrı günlüğü okunamadı: %v", err)
	}
	parts := strings.Split(string(b), callSeparator)
	return parts[:len(parts)-1]
}

func kurulumSecenekleri(t *testing.T, repo, host string, sudo bool) Options {
	t.Helper()
	return Options{
		Host:          host,
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
		Sudo:          sudo,
	}
}

// Sıra testi: BinaryDir YOK. Paket önkontrolden önce üretilseydi hata
// "bulunamadı" olurdu; sudo'nun kendi mesajı geliyorsa önkontrol, 28 MB'lık
// yüklemeden de paket üretiminden de ÖNCE koştu demektir.
func TestSudoRefusesBeforeUploadWhenAPasswordIsRequired(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeSudoFailEnv: "sudo: a password is required"})
	repo := newFakeRepo(t)
	opts := kurulumSecenekleri(t, repo, "yonetici@sunucu", true)
	opts.BinaryDir = filepath.Join(repo, "olmayan")

	err := Run(context.Background(), opts)

	if err == nil || !strings.Contains(err.Error(), "a password is required") {
		t.Fatalf("sudo'nun kendi mesajı taşınmadı: %v", err)
	}
	if strings.Contains(err.Error(), "bulunamadı") {
		t.Fatalf("paket önkontrolden önce üretildi: %v", err)
	}
	for _, c := range cagrilar(t, log) {
		if strings.Contains(c, "install.sh") {
			t.Fatalf("parola isteyen sudo'ya rağmen kurulum gönderildi: %q", c)
		}
	}
}

// Root kipi de yüklemeden önce uid'i soruyor: root olmayan bir hedef eskiden
// 28 MB yükleyip ancak install.sh'in ilk satırında düşüyordu.
func TestRootModeRefusesANonRootTargetBeforeUpload(t *testing.T) {
	log := sahteSSH(t, map[string]string{fakeSSHUIDEnv: "1000"})
	repo := newFakeRepo(t)
	opts := kurulumSecenekleri(t, repo, "yonetici@sunucu", false)
	opts.BinaryDir = filepath.Join(repo, "olmayan")

	err := Run(context.Background(), opts)

	if err == nil || !strings.Contains(err.Error(), "-sudo") {
		t.Fatalf("root olmayan hedefte -sudo önerilmedi: %v", err)
	}
	for _, c := range cagrilar(t, log) {
		if strings.Contains(c, "install.sh") {
			t.Fatalf("root olmayan hedefe kurulum gönderildi: %q", c)
		}
	}
}

func TestSudoInstallRunsTheScriptUnderSudo(t *testing.T) {
	log := sahteSSH(t, nil)
	repo := newFakeRepo(t)

	if err := Run(context.Background(), kurulumSecenekleri(t, repo, "yonetici@sunucu", true)); err != nil {
		t.Fatalf("sudo kipinde kurulum düştü: %v", err)
	}

	c := cagrilar(t, log)
	want := []string{
		"uname -m",
		"sudo -n -- bash -c " + shellQuote("id -u"),
		"sudo -n -- bash -c " + shellQuote(remoteInstall),
	}
	if strings.Join(c, "\n|\n") != strings.Join(want, "\n|\n") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
}

func TestRootInstallRunsTheScriptDirectly(t *testing.T) {
	log := sahteSSH(t, nil)
	repo := newFakeRepo(t)

	if err := Run(context.Background(), kurulumSecenekleri(t, repo, "root@sunucu", false)); err != nil {
		t.Fatalf("root kipinde kurulum düştü: %v", err)
	}

	c := cagrilar(t, log)
	want := []string{"uname -m", "id -u", remoteInstall}
	if strings.Join(c, "\n|\n") != strings.Join(want, "\n|\n") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
}

// panely-client zorlanmış komutlu, yetkisiz istemci kullanıcısı; bir kurulum
// hesabı OLAMAZ. Kullanıcı adı verilmeyen hedef ona düşüyor
// (client.DefaultSSHUser) ve kurulum anlaşılmaz biçimde zorlanmış komuta
// çarpardı.
func TestBootstrapRefusesTheClientUser(t *testing.T) {
	repo := newFakeRepo(t)
	for _, sudo := range []bool{false, true} {
		err := Run(context.Background(), kurulumSecenekleri(t, repo, "panely-client@sunucu", sudo))
		if err == nil || !strings.Contains(err.Error(), "panely-client") {
			t.Errorf("sudo=%v: panely-client ile kurulum reddedilmedi: %v", sudo, err)
		}
	}
}

// posixUnquote, POSIX kabuğunun tek tırnak ve ters bölü kurallarını
// uygular: testin Windows'ta da çalışan yarısı.
func posixUnquote(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return "KAPANMAYAN TIRNAK"
			}
			b.WriteString(s[i+1 : i+1+j])
			i += j + 1
		case '\\':
			if i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			}
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func TestShellQuoteRoundTrips(t *testing.T) {
	for _, s := range []string{remoteInstall, "id -u", "a'b", "'", "''", `x"y$z` + "`w`"} {
		if got := posixUnquote(shellQuote(s)); got != s {
			t.Errorf("shellQuote(%q) çözülünce %q", s, got)
		}
	}
}

// TestSudoCommandSurvivesThePOSIXShell: dış tırnağı uzaktaki GİRİŞ
// kabuğu çözüyor ve Debian'da useradd'ın varsayılanı /bin/sh (dash).
// Ubuntu CI'da da sh dash.
func TestSudoCommandSurvivesThePOSIXShell(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("uzak kabuk Linux'ta")
	}
	out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // sabit metin
		"printf %s "+shellQuote(remoteInstall)).Output()
	if err != nil {
		t.Fatalf("sh düştü: %v", err)
	}
	if string(out) != remoteInstall {
		t.Fatalf("sh betiği değiştirdi:\n%s", out)
	}
}

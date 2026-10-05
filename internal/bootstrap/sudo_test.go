package bootstrap

import (
	"context"
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
// Sahte ssh fakessh_test.go'da. O, argv'nin BİÇİMİNİ ve çağrı SIRASINI
// kanıtlar; gerçek sudo'yu ve uzaktaki kabuğun tırnakları nasıl çözdüğünü
// KANITLAMAZ. Onlar Linux'ta gerçek araçlarla (upload_linux_test.go) ve
// gerçek sunucuda ölçülür (K-122, K-127).

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
	if strings.Contains(err.Error(), "not found") {
		t.Fatalf("paket önkontrolden önce üretildi: %v", err)
	}
	if c := cagrilar(t, log); birKurulumCagrisiVarMi(c) {
		t.Fatalf("parola isteyen sudo'ya rağmen sunucuya yükleme yapıldı: %q", c)
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
	if c := cagrilar(t, log); birKurulumCagrisiVarMi(c) {
		t.Fatalf("root olmayan hedefe yükleme yapıldı: %q", c)
	}
}

// Sudo yalnızca yetki denetiminde ve kurulumu başlatırken: yükleme ve
// izleme bağlanan kullanıcının kendi dizininde, yetkisiz koşuyor.
func TestSudoInstallRunsOnlyTheStartUnderSudo(t *testing.T) {
	log := sahteSSH(t, nil)
	repo := newFakeRepo(t)

	if err := Run(context.Background(), kurulumSecenekleri(t, repo, "yonetici@sunucu", true)); err != nil {
		t.Fatalf("sudo kipinde kurulum düştü: %v", err)
	}

	want := []string{"uname", "yetki sudo", "hazırla", "yaz 0", "hazırla", "başlat sudo", "izle 0"}
	if c := cagrilar(t, log); strings.Join(c, "|") != strings.Join(want, "|") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
}

func TestRootInstallRunsTheScriptDirectly(t *testing.T) {
	log := sahteSSH(t, nil)
	repo := newFakeRepo(t)

	if err := Run(context.Background(), kurulumSecenekleri(t, repo, "root@sunucu", false)); err != nil {
		t.Fatalf("root kipinde kurulum düştü: %v", err)
	}

	want := []string{"uname", "yetki", "hazırla", "yaz 0", "hazırla", "başlat", "izle 0"}
	if c := cagrilar(t, log); strings.Join(c, "|") != strings.Join(want, "|") {
		t.Fatalf("çağrılar:\n%q\nbeklenen:\n%q", c, want)
	}
}

// kadran-client zorlanmış komutlu, yetkisiz istemci kullanıcısı; bir kurulum
// hesabı OLAMAZ. Kullanıcı adı verilmeyen hedef ona düşüyor
// (client.DefaultSSHUser) ve kurulum anlaşılmaz biçimde zorlanmış komuta
// çarpardı.
func TestBootstrapRefusesTheClientUser(t *testing.T) {
	repo := newFakeRepo(t)
	for _, sudo := range []bool{false, true} {
		err := Run(context.Background(), kurulumSecenekleri(t, repo, "kadran-client@sunucu", sudo))
		if err == nil || !strings.Contains(err.Error(), "kadran-client") {
			t.Errorf("sudo=%v: kadran-client ile kurulum reddedilmedi: %v", sudo, err)
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
	for _, s := range []string{remoteInstallStart, remoteInstallRun, "id -u", "a'b", "'", "''", `x"y$z` + "`w`"} {
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
	// remoteInstallRun, başlatma betiğine ARGÜMAN olarak gidiyor: tek
	// tırnaklarıyla (trap) birlikte iki kat tırnaklanmış oluyor.
	for _, s := range []string{remoteInstallStart, remoteInstallRun} {
		out, err := exec.CommandContext(t.Context(), "sh", "-c", //nolint:gosec // sabit metin
			"printf %s "+shellQuote(s)).Output()
		if err != nil {
			t.Fatalf("sh düştü: %v", err)
		}
		if string(out) != s {
			t.Fatalf("sh betiği değiştirdi:\n%s", out)
		}
	}
}

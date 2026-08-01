package bootstrap

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRejectsPrivateKey, kazara özel anahtar verilmesini yakalar.
//
// Bu kontrolün olmaması felaket olurdu: özel anahtar sunucuya yüklenir ve
// authorized_keys'e yazılırdı. Panely'nin tüm güvenlik modeli anahtar
// malzemesinin iş istasyonundan hiç çıkmamasına dayanıyor.
func TestRejectsPrivateKey(t *testing.T) {
	privateKeys := []string{
		"-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1r\n-----END OPENSSH PRIVATE KEY-----",
		"-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----",
		"-----BEGIN EC PRIVATE KEY-----\nMHcCAQ\n-----END EC PRIVATE KEY-----",
	}

	for _, key := range privateKeys {
		err := validatePublicKey([]byte(key))
		if err == nil {
			t.Fatalf("özel anahtar kabul edildi:\n%s", key)
		}
		if !strings.Contains(err.Error(), "ÖZEL anahtar") {
			t.Errorf("hata özel anahtar olduğunu söylemiyor: %v", err)
		}
	}
}

func TestAcceptsPublicKeyTypes(t *testing.T) {
	valid := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample erkan@dizustu",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQExample erkan@dizustu",
		"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYExample x",
		"sk-ssh-ed25519@openssh.com AAAAGnNrLXNzaC1lExample yubikey",
	}

	for _, key := range valid {
		if err := validatePublicKey([]byte(key)); err != nil {
			t.Errorf("geçerli anahtar reddedildi (%q): %v", key, err)
		}
	}
}

func TestRejectsGarbage(t *testing.T) {
	invalid := []string{
		"",
		"   ",
		"merhaba",
		"ssh-ed25519",
		"bilinmeyen-tur AAAAB3Nz",
	}

	for _, key := range invalid {
		if err := validatePublicKey([]byte(key)); err == nil {
			t.Errorf("geçersiz girdi kabul edildi: %q", key)
		}
	}
}

// TestArchiveCarriesEverythingTheInstallerNeeds, paketin kurulum
// betiğinin beklediği her dosyayı taşıdığını doğrular.
//
// Eksik bir dosya uzakta, kurulumun ortasında fark edilirdi — yarım
// yapılandırılmış bir sunucu bırakarak.
func TestArchiveCarriesEverythingTheInstallerNeeds(t *testing.T) {
	repo := newFakeRepo(t)

	archive, err := buildArchive(Options{
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
	}, "arm64")
	if err != nil {
		t.Fatalf("paket üretilemedi: %v", err)
	}

	files := readArchive(t, archive)

	// install.sh bu adlarla okuyor; listeler ayrışırsa kurulum uzakta ölür.
	required := []string{
		"install.sh",
		"panelyd", "panely-exec", "panely-connect",
		"panelyd.service", "panely-exec.service", "panely-tmpfiles.conf",
		"client_key.pub",
	}
	for _, name := range required {
		if _, ok := files[name]; !ok {
			t.Errorf("pakette %q yok", name)
		}
	}

	// İş istasyonu aracı sunucuda işi olmayan bir binary: ayrıcalıklı
	// makinedeki yüzeyi gereksiz büyütür.
	if _, ok := files["panely"]; ok {
		t.Error("iş istasyonu aracı sunucu paketine girmiş")
	}
}

// TestArchiveUsesMatchingArchitecture, doğru mimarinin seçildiğini
// doğrular.
//
// Yanlış mimaride bir binary "exec format error" ile ölür ve neden
// günlükte kolayca gözden kaçar.
func TestArchiveUsesMatchingArchitecture(t *testing.T) {
	repo := newFakeRepo(t)

	for _, arch := range []string{"amd64", "arm64"} {
		archive, err := buildArchive(Options{
			BinaryDir:     filepath.Join(repo, "bin"),
			RepoRoot:      repo,
			ClientKeyPath: filepath.Join(repo, "key.pub"),
		}, arch)
		if err != nil {
			t.Fatalf("%s paketi üretilemedi: %v", arch, err)
		}

		files := readArchive(t, archive)
		want := "panelyd-" + arch
		if got := strings.TrimSpace(string(files["panelyd"])); got != want {
			t.Errorf("%s için yanlış binary: %q, beklenen %q", arch, got, want)
		}
	}
}

func TestArchiveFailsClearlyOnMissingBinary(t *testing.T) {
	repo := newFakeRepo(t)

	// arm64 var, amd64 yok.
	_, err := buildArchive(Options{
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
	}, "s390x")
	if err == nil {
		t.Fatal("olmayan mimari için paket üretildi")
	}
	// Hata ne yapılacağını söylemeli.
	if !strings.Contains(err.Error(), "build-release") {
		t.Errorf("hata çözümü göstermiyor: %v", err)
	}
}

// TestUnitFilesAreNormalizedToLF, satır sonlarının dönüştürüldüğünü
// doğrular.
//
// Geliştirme Windows'ta yapılıyor. CRLF taşıyan bir systemd birimi ya da
// kabuk betiği Linux'ta sessizce bozulur; hata mesajı da nedeni
// göstermez.
func TestUnitFilesAreNormalizedToLF(t *testing.T) {
	repo := newFakeRepo(t)

	// Birimi kasten CRLF ile yaz.
	unit := filepath.Join(repo, "deploy", "systemd", "panelyd.service")
	if err := os.WriteFile(unit, []byte("[Unit]\r\nDescription=Panely\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	archive, err := buildArchive(Options{
		BinaryDir:     filepath.Join(repo, "bin"),
		RepoRoot:      repo,
		ClientKeyPath: filepath.Join(repo, "key.pub"),
	}, "arm64")
	if err != nil {
		t.Fatalf("paket üretilemedi: %v", err)
	}

	files := readArchive(t, archive)
	if bytes.Contains(files["panelyd.service"], []byte("\r")) {
		t.Error("systemd birimi CRLF taşıyor — Linux'ta sessizce bozulur")
	}
}

// TestInstallScriptKeepsPrimaryGroupInvariant, kurulum betiğinin
// birincil grup değişmezini koruduğunu doğrular.
//
// `useradd -G panely-client` (ek grup) yazılırsa SO_PEERCRED grubu
// göremez ve HER bağlantı sessizce reddedilir — hata mesajı olmadan.
// Bu, projedeki en pahalı sessiz hata adayı.
func TestInstallScriptKeepsPrimaryGroupInvariant(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, "--gid panely-client") {
		t.Error("istemci kullanıcısı birincil grupla (--gid) oluşturulmuyor")
	}
	// -G / --groups ile panely-client vermek sessiz arızaya yol açar.
	if strings.Contains(text, "--groups panely-client") || strings.Contains(text, "-G panely-client") {
		t.Error("istemci kullanıcısına panely-client EK grup olarak verilmiş")
	}
	// Kurulum sonunda doğrulama yapmalı.
	if !strings.Contains(text, "id -gn panely-client") {
		t.Error("kurulum betiği birincil grubu doğrulamıyor")
	}
}

// TestInstallScriptForcesConnectCommand, SSH erişiminin zorlanmış
// komutla sınırlandığını doğrular.
func TestInstallScriptForcesConnectCommand(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}
	text := string(script)

	if !strings.Contains(text, `command=\"$LIB_DIR/panely-connect\"`) {
		t.Error("authorized_keys satırı zorlanmış komut içermiyor")
	}
	if !strings.Contains(text, "restrict") {
		t.Error("authorized_keys satırında `restrict` yok")
	}
	// ExposeAuthInfo olmadan denetim kaydındaki parmak izi boş kalır.
	if !strings.Contains(text, "ExposeAuthInfo yes") {
		t.Error("sshd yapılandırmasında ExposeAuthInfo yok")
	}
	// sshd'yi doğrulamadan yeniden yüklemek bizi dışarıda bırakabilir.
	if !strings.Contains(text, `"$SSHD_BIN" -t`) {
		t.Error("sshd yapılandırması yeniden yüklemeden önce doğrulanmıyor")
	}
}

// TestInstallScriptDoesNotUseNologinForClient, istemci kabuğunun
// nologin OLMADIĞINI doğrular.
//
// sshd zorlanmış komutu kullanıcının giriş kabuğu üzerinden çalıştırır;
// nologin onu reddeder ve taşıma tamamen çalışmaz. Hesabı kısıtlayan şey
// kabuk değil, `command=...,restrict` ikilisi.
func TestInstallScriptDoesNotUseNologinForClient(t *testing.T) {
	script, err := installScript.ReadFile("install.sh")
	if err != nil {
		t.Fatal(err)
	}

	clientBlock := between(string(script), "--gid panely-client", "panely-client")
	if strings.Contains(clientBlock, "NOLOGIN") {
		t.Error("istemci kullanıcısına nologin verilmiş — zorlanmış komut çalışmaz")
	}
}

func between(text, start, end string) string {
	i := strings.Index(text, start)
	if i < 0 {
		return ""
	}
	rest := text[i:]
	j := strings.Index(rest[len(start):], end)
	if j < 0 {
		return rest
	}
	return rest[:len(start)+j]
}

// ── Yardımcılar ──────────────────────────────────────────────────────

// newFakeRepo, buildArchive'ın beklediği düzende sahte bir depo kurar.
func newFakeRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	for _, arch := range []string{"amd64", "arm64"} {
		dir := filepath.Join(root, "bin", "linux-"+arch)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range serverBinaries {
			content := name + "-" + arch
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}

	systemd := filepath.Join(root, "deploy", "systemd")
	if err := os.MkdirAll(systemd, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, rel := range unitFiles {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.WriteFile(path, []byte("# sahte birim\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	key := "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample erkan@test\n"
	if err := os.WriteFile(filepath.Join(root, "key.pub"), []byte(key), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func readArchive(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()

	files := make(map[string][]byte)
	tr := tar.NewReader(bytes.NewReader(archive))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("paket okunamadı: %v", err)
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("%s okunamadı: %v", header.Name, err)
		}
		files[header.Name] = content
	}
	return files
}

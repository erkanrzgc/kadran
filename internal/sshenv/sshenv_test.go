package sshenv

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io/fs"
	"strings"
	"testing"
)

// fakeEnv, os.Getenv yerine geçen test yardımcısı.
func fakeEnv(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
}

// fakeFiles, os.ReadFile yerine geçen test yardımcısı.
func fakeFiles(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		c, ok := files[p]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return []byte(c), nil
	}
}

// ed25519 anahtar blobunu taklit eden sabit bir bayt dizisi. Gerçek bir
// anahtar olması gerekmiyor: parmak izi ham blobun SHA-256'sıdır ve
// hesaplama içeriğe bakmaz.
var testBlob = []byte("panely-test-key-blob-0123456789")

func testBlobB64() string { return base64.StdEncoding.EncodeToString(testBlob) }

func expectedFingerprint() string {
	sum := sha256.Sum256(testBlob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

const authPath = "/tmp/sshauth.Xk3q9"

// withAuthFile, sshd'nin ExposeAuthInfo ile yaptığını taklit eder: dosyayı
// yazar, yolunu SSH_USER_AUTH'a koyar.
func withAuthFile(content string) (func(string) string, func(string) ([]byte, error)) {
	return fakeEnv(map[string]string{
			"SSH_CONNECTION": "203.0.113.7 54321 10.0.0.5 22",
			"SSH_USER_AUTH":  authPath,
		}),
		fakeFiles(map[string]string{authPath: content})
}

func TestParseExtractsSourceIP(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION": "203.0.113.7 54321 10.0.0.5 22",
	}), fakeFiles(nil))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}
	if id.SourceIP != "203.0.113.7" {
		t.Errorf("kaynak IP = %q, beklenen 203.0.113.7", id.SourceIP)
	}
}

// TestParseReadsSSHUserAuthFile: OpenSSH ExposeAuthInfo'yu bir DOSYAYLA
// bildirir, yolu SSH_USER_AUTH'tadır (sshd_config(5), sunucuda okundu).
// Kod eskiden SSH_AUTH_INFO_0'ı okuyordu; o PAM'in iç değişkeni, oturuma
// gelmiyor. Sonuç: canlıda 56 SSH kaydının 0'ında parmak izi vardı (K-134).
func TestParseReadsSSHUserAuthFile(t *testing.T) {
	id, err := Parse(withAuthFile("publickey ssh-ed25519 " + testBlobB64() + "\n"))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}
	if want := expectedFingerprint(); id.Fingerprint != want {
		t.Errorf("parmak izi = %q, beklenen %q", id.Fingerprint, want)
	}
	if id.KeyType != "ssh-ed25519" {
		t.Errorf("anahtar türü = %q", id.KeyType)
	}
}

// TestSSHAuthInfo0IsNotTheMechanism: SSH_AUTH_INFO_0 tek başına parmak izi
// VERMEZ. Tek bir mekanizma: sshd'nin belgelediği dosya.
func TestSSHAuthInfo0IsNotTheMechanism(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION":  "203.0.113.7 54321 10.0.0.5 22",
		"SSH_AUTH_INFO_0": "publickey ssh-ed25519 " + testBlobB64(),
	}), fakeFiles(nil))
	if err != nil {
		t.Fatal(err)
	}
	if id.Fingerprint != "" {
		t.Errorf("SSH_AUTH_INFO_0'dan parmak izi üretildi: %q", id.Fingerprint)
	}
}

// TestFirstPublickeyLineWins: AuthenticationMethods birden fazla yöntem
// isteyebilir; dosyada her biri bir satır. Açık anahtar satırı alınır.
func TestFirstPublickeyLineWins(t *testing.T) {
	other := base64.StdEncoding.EncodeToString([]byte("baska-anahtar"))
	id, err := Parse(withAuthFile("keyboard-interactive\npublickey ssh-ed25519 " + testBlobB64() +
		"\npublickey ssh-ed25519 " + other + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if id.Fingerprint != expectedFingerprint() {
		t.Errorf("parmak izi = %q, ilk publickey satırınınki bekleniyordu", id.Fingerprint)
	}
}

// TestFingerprintMatchesSSHKeygenFormat, üretilen parmak izinin
// `ssh-keygen -lf` biçimiyle aynı olduğunu doğrular.
//
// Bu önemli: kullanıcı denetim günlüğünde gördüğü değeri kendi
// anahtarıyla doğrudan karşılaştırabilmeli. Farklı bir kodlama (hex,
// dolgulu base64, MD5) kaydı okunabilir olmaktan çıkarırdı.
func TestFingerprintMatchesSSHKeygenFormat(t *testing.T) {
	id, err := Parse(withAuthFile("publickey ssh-ed25519 " + testBlobB64()))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}

	// ssh-keygen biçimi: "SHA256:" + dolgusuz base64, 43 karakter.
	const prefix = "SHA256:"
	if len(id.Fingerprint) != len(prefix)+43 {
		t.Errorf("parmak izi uzunluğu = %d, ssh-keygen biçimi 50 karakter olmalı", len(id.Fingerprint))
	}
	for _, c := range id.Fingerprint[len(prefix):] {
		if c == '=' {
			t.Error("parmak izinde dolgu karakteri var — ssh-keygen dolgusuz base64 kullanır")
		}
	}
}

func TestParseRequiresSSHConnection(t *testing.T) {
	_, err := Parse(fakeEnv(map[string]string{}), fakeFiles(nil))
	if !errors.Is(err, ErrNoConnectionInfo) {
		t.Fatalf("ErrNoConnectionInfo bekleniyordu, %v alındı", err)
	}
}

// TestMissingAuthInfoIsNotFatal, ExposeAuthInfo kapalıyken (SSH_USER_AUTH
// yok) ya da dosya okunamazken bağlantının reddedilmediğini doğrular.
// Denetim kaydı yalnızca daha az bilgi taşır; boş parmak izi "bilinmiyor"
// demektir ve bu dürüsttür.
func TestMissingAuthInfoIsNotFatal(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"değişken yok": {"SSH_CONNECTION": "203.0.113.7 54321 10.0.0.5 22"},
		"dosya yok":    {"SSH_CONNECTION": "203.0.113.7 54321 10.0.0.5 22", "SSH_USER_AUTH": "/tmp/yok"},
	} {
		id, err := Parse(fakeEnv(env), fakeFiles(nil))
		if err != nil {
			t.Fatalf("%s: hata döndü: %v", name, err)
		}
		if id.Fingerprint != "" {
			t.Errorf("%s: parmak izi uyduruldu: %q", name, id.Fingerprint)
		}
		if id.SourceIP == "" {
			t.Errorf("%s: IP yine de çıkarılmalıydı", name)
		}
	}
}

func TestMalformedAuthInfoLeavesFingerprintEmpty(t *testing.T) {
	tests := []struct {
		name string
		auth string
	}{
		{"eksik alan", "publickey ssh-ed25519"},
		{"bozuk base64", "publickey ssh-ed25519 !!!bu-base64-degil!!!"},
		{"boş", " "},
		{"yalnız parola", "password"},
		{"çok büyük", "publickey ssh-ed25519 " + testBlobB64() + "\n" + strings.Repeat("x", maxAuthFile)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := Parse(withAuthFile(tc.auth))
			if err != nil {
				t.Fatalf("bozuk auth info bağlantıyı kesti: %v", err)
			}
			if id.Fingerprint != "" {
				t.Errorf("bozuk girdiden parmak izi üretildi: %q", id.Fingerprint)
			}
		})
	}
}

func TestMalformedConnectionStringYieldsEmptyIP(t *testing.T) {
	// Boşluk dizesi SSH_CONNECTION olarak "tanımlı" sayılır ama alan
	// içermez. Çökmeden boş IP dönmeli.
	id, err := Parse(fakeEnv(map[string]string{"SSH_CONNECTION": "   "}), fakeFiles(nil))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}
	if id.SourceIP != "" {
		t.Errorf("kaynak IP = %q, beklenen boş", id.SourceIP)
	}
}

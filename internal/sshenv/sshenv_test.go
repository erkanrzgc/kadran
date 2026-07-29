package sshenv

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"
)

// fakeEnv, os.Getenv yerine geçen test yardımcısı.
func fakeEnv(pairs map[string]string) func(string) string {
	return func(k string) string { return pairs[k] }
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

func TestParseExtractsSourceIP(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION": "203.0.113.7 54321 10.0.0.5 22",
	}))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}
	if id.SourceIP != "203.0.113.7" {
		t.Errorf("kaynak IP = %q, beklenen 203.0.113.7", id.SourceIP)
	}
}

func TestParseExtractsFingerprint(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION":  "203.0.113.7 54321 10.0.0.5 22",
		"SSH_AUTH_INFO_0": "publickey ssh-ed25519 " + testBlobB64(),
	}))
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

// TestFingerprintMatchesSSHKeygenFormat, üretilen parmak izinin
// `ssh-keygen -lf` biçimiyle aynı olduğunu doğrular.
//
// Bu önemli: kullanıcı denetim günlüğünde gördüğü değeri kendi
// anahtarıyla doğrudan karşılaştırabilmeli. Farklı bir kodlama (hex,
// dolgulu base64, MD5) kaydı okunabilir olmaktan çıkarırdı.
func TestFingerprintMatchesSSHKeygenFormat(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION":  "10.0.0.1 1 10.0.0.2 22",
		"SSH_AUTH_INFO_0": "publickey ssh-ed25519 " + testBlobB64(),
	}))
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
	_, err := Parse(fakeEnv(map[string]string{}))
	if !errors.Is(err, ErrNoConnectionInfo) {
		t.Fatalf("ErrNoConnectionInfo bekleniyordu, %v alındı", err)
	}
}

// TestMissingAuthInfoIsNotFatal, ExposeAuthInfo kapalıyken bağlantının
// reddedilmediğini doğrular.
//
// SSH_AUTH_INFO_0 sshd_config'de `ExposeAuthInfo yes` gerektirir. Eksikse
// denetim kaydı sadece daha az bilgi taşır; bağlantıyı kesmek için sebep
// değildir. Boş parmak izi "bilinmiyor" demektir ve bu dürüsttür.
func TestMissingAuthInfoIsNotFatal(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION": "203.0.113.7 54321 10.0.0.5 22",
	}))
	if err != nil {
		t.Fatalf("SSH_AUTH_INFO_0 eksikken hata döndü: %v", err)
	}
	if id.Fingerprint != "" {
		t.Errorf("parmak izi uyduruldu: %q", id.Fingerprint)
	}
	if id.SourceIP == "" {
		t.Error("IP yine de çıkarılmalıydı")
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
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := Parse(fakeEnv(map[string]string{
				"SSH_CONNECTION":  "10.0.0.1 1 10.0.0.2 22",
				"SSH_AUTH_INFO_0": tc.auth,
			}))
			if err != nil {
				t.Fatalf("bozuk auth info bağlantıyı kesti: %v", err)
			}
			if id.Fingerprint != "" {
				t.Errorf("bozuk girdiden parmak izi üretildi: %q", id.Fingerprint)
			}
		})
	}
}

// TestNonPublickeyMethodIsRejected, Panely'nin yalnızca açık anahtarla
// girişi tanıdığını doğrular. Parola ile giriş görülürse parmak izi
// üretilmez — uydurulmuş bir kimlik yerine boş kimlik.
func TestNonPublickeyMethodIsRejected(t *testing.T) {
	id, err := Parse(fakeEnv(map[string]string{
		"SSH_CONNECTION":  "10.0.0.1 1 10.0.0.2 22",
		"SSH_AUTH_INFO_0": "password",
	}))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}
	if id.Fingerprint != "" {
		t.Errorf("parola yönteminden parmak izi üretildi: %q", id.Fingerprint)
	}
}

func TestMalformedConnectionStringYieldsEmptyIP(t *testing.T) {
	// Boşluk dizesi SSH_CONNECTION olarak "tanımlı" sayılır ama alan
	// içermez. Çökmeden boş IP dönmeli.
	id, err := Parse(fakeEnv(map[string]string{"SSH_CONNECTION": "   "}))
	if err != nil {
		t.Fatalf("çözümleme başarısız: %v", err)
	}
	if id.SourceIP != "" {
		t.Errorf("kaynak IP = %q, beklenen boş", id.SourceIP)
	}
}

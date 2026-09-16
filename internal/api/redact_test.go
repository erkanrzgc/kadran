package api

import (
	"testing"

	"github.com/erkanrzgc/panely/internal/audit"
)

// Bu testler `internal/audit`'ten TAŞINDI. Sebebi redact.go'nun başında
// yazılı: politika ayrıcalıklı ikilinin yüzeyinde durmamalı. Testler de
// kodun yanına geldi — ayrı kalsalardı, taşınan kodun testsiz kaldığı
// izlenimi doğardı.

// TestIsSensitiveKeyRecognisesRealNames, sezgiselin gerçek dünyada
// karşılaşılan adları yakaladığını doğrular.
func TestIsSensitiveKeyRecognisesRealNames(t *testing.T) {
	sensitive := []string{
		"PASSWORD", "DB_PASSWORD", "db-password", "dbPassword",
		"SECRET_KEY", "JWT_SECRET", "API_KEY", "APIKEY", "apiKey",
		"GITHUB_TOKEN", "AWS_SECRET_ACCESS_KEY",
		"PRIVATE_KEY", "SSH_PRIVATE_KEY",
		"DATABASE_DSN", "CONNECTION_STRING",
		"AUTH_HEADER", "SESSION_SECRET", "TOTP_SEED",
		"key", "PIN", "otp", "TLS_CERT", "COOKIE_SECRET",
	}
	for _, name := range sensitive {
		if !isSensitiveKey(name) {
			t.Errorf("%q sır olarak tanınmadı", name)
		}
	}
}

// TestIsSensitiveKeyLeavesOrdinaryNames, sezgiselin her şeyi sır saymadığını
// doğrular.
//
// Bu POZİTİF KONTROLDÜR. O olmadan `return true` yazan bir uygulama
// üstteki testi geçerdi ve denetim kaydı tamamen okunmaz hâle gelirdi.

// TestIsSensitiveKeyLeavesOrdinaryNames, sezgiselin her şeyi sır saymadığını
// doğrular.
//
// Bu POZİTİF KONTROLDÜR. O olmadan `return true` yazan bir uygulama
// üstteki testi geçerdi ve denetim kaydı tamamen okunmaz hâle gelirdi.
func TestIsSensitiveKeyLeavesOrdinaryNames(t *testing.T) {
	ordinary := []string{
		"PORT", "HOST", "LANG", "PATH", "HOME", "TZ",
		"NODE_ENV", "LOG_LEVEL", "REPLICAS", "IMAGE_TAG",
		"keyboard_layout", "monkey", "turkey", "MONKEY_COUNT",
		"app_id", "release_id", "commit_sha", "container_port",
	}
	for _, name := range ordinary {
		if isSensitiveKey(name) {
			t.Errorf("%q gereksiz yere sır sayıldı", name)
		}
	}
}

// TestRedactSensitiveRedactsOnlySecrets, karışık parametrelerde
// seçici redaksiyonu doğrular.

// TestRedactSensitiveRedactsOnlySecrets, karışık parametrelerde
// seçici redaksiyonu doğrular.
func TestRedactSensitiveRedactsOnlySecrets(t *testing.T) {
	got := redactSensitive(map[string]string{
		"app_id":       "blog",
		"commit_sha":   "abc1234",
		"DB_PASSWORD":  "hunter2",
		"replica":      "0",
		"GITHUB_TOKEN": "ghp_xxx",
	})

	if got["app_id"] != "blog" {
		t.Errorf("app_id redakte edilmiş: %q — denetim izi işe yaramaz hâle gelir", got["app_id"])
	}
	if got["commit_sha"] != "abc1234" {
		t.Errorf("commit_sha redakte edilmiş: %q", got["commit_sha"])
	}
	if got["DB_PASSWORD"] != audit.Redacted {
		t.Errorf("DB_PASSWORD redakte EDİLMEMİŞ: %q", got["DB_PASSWORD"])
	}
	if got["GITHUB_TOKEN"] != audit.Redacted {
		t.Errorf("GITHUB_TOKEN redakte EDİLMEMİŞ: %q", got["GITHUB_TOKEN"])
	}
}

// TestRedactedHidesValueLength, işaretin değerin uzunluğunu da
// gizlediğini doğrular. Uzunluk tek başına bilgidir.

package audit

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestRedactEnvRemovesEveryValue, ortam değişkenlerinde varsayılan REDDET
// politikasının uygulandığını doğrular.
//
// Bu testin varlık sebebi somut: Record.ParamsJSON alanı uzun süre
// "sırlar redakte edilmiş hâlde" diyen bir yorum taşıdı ama redaksiyonu
// yapan hiçbir kod YOKTU. Yorum bir iddiaydı; bu test onu ölçüme çeviriyor.
func TestRedactEnvRemovesEveryValue(t *testing.T) {
	env := map[string]string{
		"DATABASE_URL": "postgres://user:hunter2@db:5432/app",
		"PORT":         "8080",         // masum görünüyor
		"CONFIG":       "api_key=abcd", // masum AD, sırlı DEĞER
		"LANG":         "tr_TR.UTF-8",
	}

	got := RedactEnv(env)

	if len(got) != len(env) {
		t.Fatalf("anahtar sayısı %d, beklenen %d", len(got), len(env))
	}
	for k, v := range got {
		if v != Redacted {
			t.Errorf("%q anahtarının değeri redakte edilmemiş: %q", k, v)
		}
	}

	// Hiçbir orijinal değer çıktıda geçmemeli.
	blob, err := MarshalParams(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"hunter2", "8080", "abcd", "tr_TR"} {
		if strings.Contains(blob, leak) {
			t.Errorf("redakte edilmiş çıktı %q sızdırıyor: %s", leak, blob)
		}
	}
}

// TestRedactEnvKeepsKeyNames, redaksiyonun denetim değerini yok etmediğini
// doğrular: "hangi değişkenler ayarlandı" sorusu yanıtlanabilir kalmalı.
//
// Bu, üstteki testin karşı ağırlığıdır. O test olmadan "her şeyi sil"
// da geçerdi; bu test onu reddeder.
func TestRedactEnvKeepsKeyNames(t *testing.T) {
	got := RedactEnv(map[string]string{"DATABASE_URL": "gizli", "PORT": "8080"})

	if _, ok := got["DATABASE_URL"]; !ok {
		t.Error("DATABASE_URL anahtarı kaybolmuş — denetim izi ne ayarlandığını söyleyemiyor")
	}
	if _, ok := got["PORT"]; !ok {
		t.Error("PORT anahtarı kaybolmuş")
	}
}

// TestRedactEnvDoesNotMutateInput, redaksiyonun çağıranın haritasını
// bozmadığını doğrular.
//
// Girdi haritası konteyneri gerçekten oluşturmak için de kullanılıyor.
// Yerinde değiştirmek, uygulamanın ortam değişkenlerinin değerini
// "[REDACTED]" yapardı — yani denetim kodu üretimi bozardı.
func TestRedactEnvDoesNotMutateInput(t *testing.T) {
	env := map[string]string{"DATABASE_URL": "postgres://gercek-deger"}
	_ = RedactEnv(env)

	if env["DATABASE_URL"] != "postgres://gercek-deger" {
		t.Errorf("girdi haritası değiştirilmiş: %q", env["DATABASE_URL"])
	}
}

func TestRedactEnvNilStaysNil(t *testing.T) {
	if got := RedactEnv(nil); got != nil {
		t.Errorf("nil girdi için %v döndü, nil bekleniyordu", got)
	}
}

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
		if !IsSensitiveKey(name) {
			t.Errorf("%q sır olarak tanınmadı", name)
		}
	}
}

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
		if IsSensitiveKey(name) {
			t.Errorf("%q gereksiz yere sır sayıldı", name)
		}
	}
}

// TestRedactSensitiveRedactsOnlySecrets, karışık parametrelerde
// seçici redaksiyonu doğrular.
func TestRedactSensitiveRedactsOnlySecrets(t *testing.T) {
	got := RedactSensitive(map[string]string{
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
	if got["DB_PASSWORD"] != Redacted {
		t.Errorf("DB_PASSWORD redakte EDİLMEMİŞ: %q", got["DB_PASSWORD"])
	}
	if got["GITHUB_TOKEN"] != Redacted {
		t.Errorf("GITHUB_TOKEN redakte EDİLMEMİŞ: %q", got["GITHUB_TOKEN"])
	}
}

// TestRedactedHidesValueLength, işaretin değerin uzunluğunu da
// gizlediğini doğrular. Uzunluk tek başına bilgidir.
func TestRedactedHidesValueLength(t *testing.T) {
	short := RedactEnv(map[string]string{"K": "a"})
	long := RedactEnv(map[string]string{"K": strings.Repeat("x", 4096)})

	if short["K"] != long["K"] {
		t.Errorf("redaksiyon uzunluğu sızdırıyor: %q vs %q", short["K"], long["K"])
	}
}

// TestMarshalParamsIsDeterministic, aynı girdinin her zaman aynı baytları
// verdiğini doğrular. Zincirin yeniden hesaplanabilmesi buna bağlı.
func TestMarshalParamsIsDeterministic(t *testing.T) {
	m := map[string]string{"z": "1", "a": "2", "m": "3", "b": "4"}

	first, err := MarshalParams(m)
	if err != nil {
		t.Fatal(err)
	}
	for range 20 {
		again, err := MarshalParams(m)
		if err != nil {
			t.Fatal(err)
		}
		if again != first {
			t.Fatalf("kodlama belirlenimci değil:\n  %s\n  %s", first, again)
		}
	}

	// Gerçekten geçerli JSON mu?
	var back map[string]string
	if err := json.Unmarshal([]byte(first), &back); err != nil {
		t.Fatalf("üretilen JSON çözümlenemedi: %v", err)
	}
	if len(back) != len(m) {
		t.Errorf("tur atınca %d anahtar kaldı, %d bekleniyordu", len(back), len(m))
	}
}

func TestMarshalParamsNilIsEmptyObject(t *testing.T) {
	got, err := MarshalParams(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "{}" {
		t.Errorf("nil için %q döndü, \"{}\" bekleniyordu", got)
	}
}

// TestRedactedRecordSurvivesTheChain, redakte edilmiş parametrelerin
// gerçekten zincire yazılabildiğini ve doğrulanabildiğini gösterir.
//
// Redaksiyon ile hash zinciri arasında bir uyumsuzluk olsaydı (örneğin
// redaksiyon mühürlemeden SONRA uygulansaydı) zincir kopardı.
func TestRedactedRecordSurvivesTheChain(t *testing.T) {
	params, err := MarshalParams(RedactEnv(map[string]string{
		"DATABASE_URL": "postgres://user:hunter2@db/app",
	}))
	if err != nil {
		t.Fatal(err)
	}

	rec := Seal(Record{
		TS:         time.Now().UTC(),
		Actor:      SystemActor("test"),
		Action:     "container.create",
		Target:     "app/blog",
		ParamsJSON: params,
		Outcome:    OutcomeSuccess,
		Source:     SourceExecutor,
	}, 1, GenesisHash)

	if _, err := VerifyAll([]Record{rec}); err != nil {
		t.Fatalf("redakte edilmiş kayıt zincirde doğrulanamadı: %v", err)
	}
	if strings.Contains(rec.ParamsJSON, "hunter2") {
		t.Error("zincire yazılan kayıt sırrı taşıyor")
	}
}

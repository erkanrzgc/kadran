package totp

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

var anahtar = []byte("0123456789abcdef0123456789abcdef")

// signedPayload, verilen içeriği GEÇERLİ imzayla jeton biçimine koyar.
func signedPayload(key []byte, b64 string) string {
	head := "v1." + b64
	return head + "." + base64.RawURLEncoding.EncodeToString(sign(key, head))
}

func ornekClaims() Claims {
	return Claims{Fingerprint: "SHA256:abc", Expires: time.Unix(1_800_000_000, 0), Generation: 3}
}

func TestTokenRoundTrip(t *testing.T) {
	tok := SignToken(anahtar, ornekClaims())
	if !strings.HasPrefix(tok, "v1.") || strings.Count(tok, ".") != 2 {
		t.Fatalf("jeton biçimi %q", tok)
	}
	got, err := ParseToken(anahtar, tok)
	if err != nil {
		t.Fatal(err)
	}
	if got != ornekClaims() {
		t.Fatalf("geri okunan %+v, beklenen %+v", got, ornekClaims())
	}
}

// TestTokenRejectsTamperedPayload: imza aynı kalıp içerik değişirse
// (ör. süre uzatılırsa) jeton reddedilir.
func TestTokenRejectsTamperedPayload(t *testing.T) {
	parts := strings.Split(SignToken(anahtar, ornekClaims()), ".")
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"gen":3`, `"gen":4`, 1)
	if tampered == string(raw) {
		t.Fatalf("kurcalama uygulanamadı: %s", raw)
	}
	tok := parts[0] + "." + base64.RawURLEncoding.EncodeToString([]byte(tampered)) + "." + parts[2]
	if _, err := ParseToken(anahtar, tok); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("kurcalanmış jeton: %v", err)
	}
}

func TestTokenRejectsOtherKey(t *testing.T) {
	tok := SignToken(anahtar, ornekClaims())
	if _, err := ParseToken([]byte("ffffffffffffffffffffffffffffffff"), tok); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("başka anahtarla: %v", err)
	}
}

func TestTokenRejectsMalformed(t *testing.T) {
	good := strings.Split(SignToken(anahtar, ornekClaims()), ".")
	for _, tok := range []string{
		"",
		"v1.x",
		"v2." + good[1] + "." + good[2],
		"v1.!!!." + good[2],
		"v1." + good[1] + ".!!!",
		// İmzası GEÇERLİ ama içeriği JSON değil: imza önce doğrulanıyor,
		// içerik ancak sonra çözülüyor.
		signedPayload(anahtar, base64.RawURLEncoding.EncodeToString([]byte("not json"))),
	} {
		if _, err := ParseToken(anahtar, tok); !errors.Is(err, ErrMalformedToken) {
			t.Errorf("%q: %v, beklenen ErrMalformedToken", tok, err)
		}
	}
}

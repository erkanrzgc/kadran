package totp

import (
	"testing"
	"time"
)

// rfcSecret, RFC 6238 Ek B'nin SHA1 test anahtarı.
var rfcSecret = []byte("12345678901234567890")

// TestRFC6238Vectors: RFC 6238 Ek B, SHA1 sütunu (8 hane). Algoritma
// standarttan sapsaydı doğrulayıcı uygulamaların ürettiği kod hiçbir
// zaman tutmazdı.
func TestRFC6238Vectors(t *testing.T) {
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	} {
		if got := codeAt(rfcSecret, StepAt(time.Unix(tc.unix, 0)), 8); got != tc.want {
			t.Errorf("T=%d: kod %s, RFC %s", tc.unix, got, tc.want)
		}
	}
}

func TestVerifyWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := StepAt(now)
	for _, d := range []int64{-1, 0, 1} {
		step, ok := Verify(rfcSecret, codeAt(rfcSecret, s+d, Digits), now, 0)
		if !ok || step != s+d {
			t.Errorf("adım %+d: ok=%v adım=%d, beklenen %d", d, ok, step, s+d)
		}
	}
	for _, d := range []int64{-2, 2} {
		if _, ok := Verify(rfcSecret, codeAt(rfcSecret, s+d, Digits), now, 0); ok {
			t.Errorf("adım %+d pencere dışında ama kabul edildi", d)
		}
	}
}

// TestVerifyReplay: kabul edilmiş adım (ve öncesi) bir daha geçmez.
func TestVerifyReplay(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	s := StepAt(now)
	code := codeAt(rfcSecret, s, Digits)
	if _, ok := Verify(rfcSecret, code, now, s); ok {
		t.Fatal("son kabul edilen adımın kodu yeniden kabul edildi")
	}
	if _, ok := Verify(rfcSecret, code, now, s-1); !ok {
		t.Fatal("son kabul edilenden sonraki adımın kodu reddedildi")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	for _, code := range []string{"", "12345", "1234567", "12a456", " 123456", "１２３４５６"} {
		if _, ok := Verify(rfcSecret, code, now, 0); ok {
			t.Errorf("%q kabul edildi", code)
		}
	}
}

// TestInWindow: pencerenin dışındaki ama yakın bir kod, "yanlış kod" ile
// "saat kaymış"ı ayırmaya yarar.
func TestInWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	code := codeAt(rfcSecret, StepAt(now)+2, Digits)
	if _, ok := Verify(rfcSecret, code, now, 0); ok {
		t.Fatal("iki adım ileri kod Verify'dan geçti")
	}
	if !InWindow(rfcSecret, code, now, 4) {
		t.Fatal("iki adım ileri kod ±4 pencerede bulunamadı")
	}
	if InWindow(rfcSecret, codeAt(rfcSecret, StepAt(now)+6, Digits), now, 4) {
		t.Fatal("altı adım ileri kod ±4 pencerede bulundu")
	}
}

// TestCodeAtMatchesVerify: CodeAt, doğrulayıcı uygulamanın o an
// göstereceği kod; Verify onu aynı adımla kabul eder.
func TestCodeAtMatchesVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	step, ok := Verify(rfcSecret, CodeAt(rfcSecret, now), now, 0)
	if !ok || step != StepAt(now) {
		t.Fatalf("CodeAt kodu: ok=%v adım=%d, beklenen %d", ok, step, StepAt(now))
	}
}

func TestSecretAndURI(t *testing.T) {
	s, err := NewSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != SecretSize {
		t.Fatalf("sır %d bayt, beklenen %d", len(s), SecretSize)
	}
	if got := EncodeSecret(rfcSecret); got != "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ" {
		t.Fatalf("base32 %s", got)
	}
	uri := OTPAuthURI(rfcSecret, "Kadran", "my host")
	want := "otpauth://totp/Kadran:my%20host?algorithm=SHA1&digits=6&issuer=Kadran&period=30&secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	if uri != want {
		t.Fatalf("URI\n got %s\nwant %s", uri, want)
	}
}

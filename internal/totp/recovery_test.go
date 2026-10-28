package totp

import (
	"regexp"
	"testing"
)

func TestRecoveryCodes(t *testing.T) {
	codes, err := NewRecoveryCodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != RecoveryCount {
		t.Fatalf("%d kod, beklenen %d", len(codes), RecoveryCount)
	}
	shape := regexp.MustCompile(`^[A-Z2-7]{5}-[A-Z2-7]{5}$`)
	seen := map[string]bool{}
	for _, c := range codes {
		if !shape.MatchString(c) {
			t.Errorf("biçim dışı kod %q", c)
		}
		if seen[c] {
			t.Errorf("yinelenen kod %q", c)
		}
		seen[c] = true
	}
}

// TestHashRecoveryNormalizes: kullanıcı kodu küçük harfle, tiresiz ya da
// boşluklu yazabilir; aynı özet çıkmalı.
func TestHashRecoveryNormalizes(t *testing.T) {
	want := HashRecovery("ABCDE-FGHIJ")
	for _, in := range []string{"abcde-fghij", "ABCDEFGHIJ", " abcde fghij "} {
		if HashRecovery(in) != want {
			t.Errorf("%q farklı özet verdi", in)
		}
	}
	if HashRecovery("ABCDE-FGHIK") == want {
		t.Fatal("farklı kod aynı özeti verdi")
	}
}

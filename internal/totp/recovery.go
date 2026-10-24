package totp

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
)

// RecoveryCount, bir kurulumda üretilen kurtarma kodu sayısı.
const RecoveryCount = 10

// NewRecoveryCodes, RecoveryCount farklı kurtarma kodu üretir.
//
// Her kod 10 base32 karakteri (50 bit) ve `XXXXX-XXXXX` biçiminde. Kodlar
// rastgele ve yüksek entropili, bu yüzden SHA-256 ile saklamak yeter;
// parolalar gibi yavaş bir özet gerekmez.
func NewRecoveryCodes() ([]string, error) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	seen := make(map[string]bool, RecoveryCount)
	codes := make([]string, 0, RecoveryCount)
	for len(codes) < RecoveryCount {
		var raw [8]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return nil, fmt.Errorf("could not generate recovery codes: %w", err)
		}
		s := enc.EncodeToString(raw[:])[:10]
		code := s[:5] + "-" + s[5:]
		if seen[code] {
			continue
		}
		seen[code] = true
		codes = append(codes, code)
	}
	return codes, nil
}

// recoveryCleaner, kullanıcının yazımındaki tireleri ve boşlukları atar.
var recoveryCleaner = strings.NewReplacer("-", "", " ", "")

// HashRecovery, kurtarma kodunun saklanan özetidir. Büyük/küçük harf,
// tire ve boşluk farkı aynı özeti verir.
func HashRecovery(code string) [32]byte {
	return sha256.Sum256([]byte(strings.ToUpper(recoveryCleaner.Replace(code))))
}

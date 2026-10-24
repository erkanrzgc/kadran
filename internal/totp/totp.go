// Package totp, iki aşamalı girişin saf parçalarıdır: RFC 6238 kodları,
// kurtarma kodları, oturum jetonu ve kilit süresi (K-155).
//
// Durum tutmaz ve G/Ç yapmaz; sır, son adım ve sayaçlar store'da. Bu
// yüzden her kural sahte saatle, ağsız sınanabiliyor.
package totp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 ve doğrulayıcı uygulamalar SHA1 kullanıyor; HMAC-SHA1 çakışmaya dayanmaz
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

const (
	// Period, bir kodun geçerli olduğu adım.
	Period = 30 * time.Second
	// Digits, kodun hane sayısı.
	Digits = 6
	// SecretSize, sırrın bayt uzunluğu (RFC 4226'nın önerdiği 160 bit).
	SecretSize = 20
)

// NewSecret, yeni bir rastgele sır üretir.
func NewSecret() ([]byte, error) {
	s := make([]byte, SecretSize)
	if _, err := rand.Read(s); err != nil {
		return nil, fmt.Errorf("could not generate a TOTP secret: %w", err)
	}
	return s, nil
}

// EncodeSecret, sırrı doğrulayıcı uygulamaların beklediği base32 ile yazar.
func EncodeSecret(secret []byte) string {
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(secret)
}

// OTPAuthURI, doğrulayıcı uygulamaya eklenecek `otpauth://` adresidir.
func OTPAuthURI(secret []byte, issuer, account string) string {
	q := url.Values{}
	q.Set("secret", EncodeSecret(secret))
	q.Set("issuer", issuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(Digits))
	q.Set("period", strconv.Itoa(int(Period/time.Second)))
	return "otpauth://totp/" + url.PathEscape(issuer) + ":" + url.PathEscape(account) + "?" + q.Encode()
}

// StepAt, t anının adım numarasıdır.
func StepAt(t time.Time) int64 { return t.Unix() / int64(Period/time.Second) }

// codeAt, RFC 4226'nın kesme işlemiyle adımın kodunu üretir.
func codeAt(secret []byte, step int64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // adım negatif olmaz (Unix zamanı / 30)
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for range digits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

// wellFormed, kodun tam Digits ASCII rakam olduğunu söyler.
func wellFormed(code string) bool {
	if len(code) != Digits {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// Verify, kodu şimdiki adım ve bir öncesi/sonrasıyla karşılaştırır.
//
// Yalnız lastStep'ten SONRAKİ adımlar kabul edilir: kabul edilmiş bir kod
// (ya da daha eskisi) ikinci kez geçmez. Dönen adım, eşleşen adımdır;
// çağıran onu kalıcı olarak son adım yapmalı.
func Verify(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	if !wellFormed(code) {
		return 0, false
	}
	s := StepAt(now)
	for step := s - 1; step <= s+1; step++ {
		if step <= lastStep {
			continue
		}
		if hmac.Equal([]byte(codeAt(secret, step, Digits)), []byte(code)) {
			return step, true
		}
	}
	return 0, false
}

// InWindow, kodun ±span adım içinde bir adıma uyup uymadığını söyler.
//
// Yalnız tanı için: pencere dışında ama yakın bir kod, yanlış koddan çok
// kaymış bir saati gösterir. Kabul kararı HER ZAMAN Verify'ındır.
func InWindow(secret []byte, code string, now time.Time, span int64) bool {
	if !wellFormed(code) {
		return false
	}
	s := StepAt(now)
	for step := s - span; step <= s+span; step++ {
		if hmac.Equal([]byte(codeAt(secret, step, Digits)), []byte(code)) {
			return true
		}
	}
	return false
}

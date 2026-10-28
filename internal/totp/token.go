package totp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	// TokenLifetime, bir girişin geçerli kaldığı süre (bir iş günü).
	TokenLifetime = 8 * time.Hour
	// TokenKeySize, jeton imza anahtarının bayt uzunluğu.
	TokenKeySize = 32
	// SessionMetadataKey, jetonun gRPC'de taşındığı metadata anahtarı.
	// İstemci yazar, kadrand okur; tek kaynak burası.
	SessionMetadataKey = "kadran-session"

	tokenVersion = "v1"
)

// Jeton hataları.
var (
	ErrMalformedToken = errors.New("malformed session token")
	ErrBadSignature   = errors.New("session token signature does not match")
)

// Claims, jetonun taşıdığı iddialardır.
//
// Jeton bir TAŞIYICI kimlik bilgisi: metadata'yı uzak istemci yazıyor.
// Bu yüzden ancak bağlantının kendi parmak iziyle (önsöz) birlikte
// geçerli; o karşılaştırma çağıranın işi.
type Claims struct {
	Fingerprint string
	Expires     time.Time
	Generation  int64
}

type payload struct {
	FP  string `json:"fp"`
	Exp int64  `json:"exp"`
	Gen int64  `json:"gen"`
}

func sign(key []byte, msg string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return mac.Sum(nil)
}

// SignToken, iddiaları `v1.<içerik>.<imza>` biçiminde imzalar.
func SignToken(key []byte, c Claims) string {
	// Yalnız dize ve tamsayı alanları: Marshal hata vermez.
	raw, _ := json.Marshal(payload{FP: c.Fingerprint, Exp: c.Expires.Unix(), Gen: c.Generation})
	head := tokenVersion + "." + base64.RawURLEncoding.EncodeToString(raw)
	return head + "." + base64.RawURLEncoding.EncodeToString(sign(key, head))
}

// ParseToken, imzayı doğrular ve iddiaları döndürür.
//
// İmza İÇERİKTEN ÖNCE doğrulanıyor: imzasız bir içerik hiç çözülmez.
// Sürenin ve kuşağın denetimi çağıranın işi.
func ParseToken(key []byte, token string) (Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != tokenVersion {
		return Claims{}, ErrMalformedToken
	}
	got, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrMalformedToken
	}
	if !hmac.Equal(got, sign(key, parts[0]+"."+parts[1])) {
		return Claims{}, ErrBadSignature
	}
	var p payload
	if err := json.Unmarshal(raw, &p); err != nil {
		return Claims{}, ErrMalformedToken
	}
	return Claims{Fingerprint: p.FP, Expires: time.Unix(p.Exp, 0), Generation: p.Gen}, nil
}

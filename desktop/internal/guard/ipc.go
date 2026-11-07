package guard

import (
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Sınırlar (Electron'daki ipc-validate.ts ve server-list.ts ile aynı).
const (
	MaxTarget   = 500
	MaxCopy     = 1024
	MaxName     = 200
	MaxProvider = 64
	MaxTail     = 10000
)

// maxSafeInteger, JavaScript'te kayıpsız tam sayı sınırı (2^53 - 1).
const maxSafeInteger = 1<<53 - 1

// WindowActions, özel başlık çubuğunun düğmeleri.
var WindowActions = []string{"minimize", "maximize", "close"}

// invisible: denetim (Cc) ve biçim (Cf, bidi dahil) karakterleri yerel onay
// kutusunda "Server:" satırını yanıltabilir; ssh hedefinde zaten anlamsızlar.
var invisible = regexp.MustCompile(`[\p{Cc}\p{Cf}]`)

var (
	appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	// BranchPattern, dal, etiket ya da tam commit; sunucunun kuralıyla aynı.
	BranchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

// Target, bir sunucu hedefini doğrular ve kırpılmış hâlini döner.
func Target(value string) (string, error) {
	if jsTrim(value) == "" {
		return "", errors.New("target is empty")
	}
	if utf16Len(value) > MaxTarget {
		return "", errors.New("target is too long")
	}
	trimmed := jsTrim(value)
	if invisible.MatchString(trimmed) {
		return "", errors.New("target must not contain control or invisible characters")
	}
	// ssh seçeneği olarak okunur (-oProxyCommand=…): savunma derinliği, ssh
	// katmanı da reddeder.
	if strings.HasPrefix(trimmed, "-") {
		return "", errors.New(`target must not start with "-"`)
	}
	return trimmed, nil
}

// Uint, arayüzün gönderdiği sayfa numarası gibi bir değeri yumuşakça okur:
// negatif, sonsuz ya da sayı olmayan değer varsayılana döner, kesir aşağı
// yuvarlanır. Sayı metni de kabul edilir (JavaScript'teki Number()).
func Uint(raw json.RawMessage, fallback uint64) uint64 {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback
	}
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case string:
		s := jsTrim(t)
		if s == "" {
			f = 0
		} else if parsed, err := strconv.ParseFloat(s, 64); err == nil {
			f = parsed
		} else {
			return fallback
		}
	case bool:
		if t {
			f = 1
		}
	case nil:
		f = 0
	default:
		return fallback
	}
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 {
		return fallback
	}
	f = math.Floor(f)
	if f > maxSafeInteger {
		return fallback
	}
	return uint64(f)
}

// WindowAction, başlık çubuğu düğmesinin beyaz listede olduğunu denetler.
func WindowAction(value string) (string, error) {
	for _, a := range WindowActions {
		if value == a {
			return value, nil
		}
	}
	return "", errors.New("unknown window action")
}

// CopyText, panoya yazılacak kısa metin (parmak izi gibi).
func CopyText(value string) (string, error) {
	if value == "" || utf16Len(value) > MaxCopy {
		return "", errors.New("copy text must be 1-1024 characters")
	}
	return value, nil
}

// AppID, uygulama adı: küçük harfle başlar, en çok 32 karakter.
func AppID(value string) (string, error) {
	if !appIDPattern.MatchString(value) {
		return "", errors.New("invalid app ID")
	}
	return value, nil
}

// Confirmation, silme ve geri alma için yazılan onay uygulama adıyla AYNI olmalı.
func Confirmation(appID, value string) error {
	if value != appID {
		return errors.New("confirmation must match the app ID exactly")
	}
	return nil
}

// Ref, dağıtılacak dal, etiket ya da tam commit; boşsa sunucunun varsayılanı.
func Ref(value string) (string, error) {
	if value != "" && !BranchPattern.MatchString(value) {
		return "", errors.New("invalid branch, tag or full commit")
	}
	return value, nil
}

// Tail, okunacak log satırı sayısı.
func Tail(value int) (int, error) {
	if value < 1 || value > MaxTail {
		return 0, errors.New("log tail must be 1-10000 lines")
	}
	return value, nil
}

// ServerName, kayıtlı sunucunun adı. Asıl kural (64 karakter, tekillik) Go
// tarafında internal/servers'ta; burada yalnız kaba bir üst sınır.
func ServerName(value string) (string, error) {
	if jsTrim(value) == "" {
		return "", errors.New("server name is empty")
	}
	if utf16Len(value) > MaxName {
		return "", errors.New("server name is too long")
	}
	return value, nil
}

// Provider, elle yazılan sağlayıcı etiketi; boş olabilir.
func Provider(value string) (string, error) {
	if runeCount(value) > MaxProvider {
		return "", errors.New("provider is too long")
	}
	return value, nil
}

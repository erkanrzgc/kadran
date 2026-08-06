package audit

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Redacted, denetim kaydında bir değerin yerine yazılan işarettir.
//
// Değerin UZUNLUĞU da sızdırılmaz: "hunter2" ve 64 baytlık bir API
// anahtarı aynı işarete dönüşür. Uzunluk tek başına bile bilgidir.
const Redacted = "[REDACTED]"

// sensitiveFragments, adı sır ima eden anahtar parçalarıdır.
//
// Liste kasıtlı olarak GENİŞ tutulmuştur. Yanlış pozitif bir redaksiyonun
// bedeli, denetim kaydında bir değerin görünmemesidir. Yanlış negatifin
// bedeli, sırrın kalıcı ve hash-zincirli bir günlüğe düz metin yazılması —
// yani geri alınamaz bir sızıntı. Bu takas simetrik değildir.
var sensitiveFragments = []string{
	"password", "passwd", "pwd",
	"secret", "token", "apikey", "api_key",
	"credential", "auth", "session",
	"private", "signature", "salt",
	"dsn", "conn_str", "connection_string",
	"cert", "cookie", "license",
}

// sensitiveExact, tek başına anahtar olduğunda sır sayılan adlardır.
//
// Bunlar `sensitiveFragments` içine parça olarak konulamaz: "key" parçası
// "keyboard_layout" veya "monkey" gibi zararsız adları da yakalardı.
var sensitiveExact = []string{
	"key", "pass", "pin", "otp", "totp", "seed",
}

// IsSensitiveKey, anahtar adının sır ima edip etmediğini söyler.
//
// Karşılaştırma büyük/küçük harf duyarsızdır ve ayırıcılar normalize
// edilir: "DB_PASSWORD", "db-password" ve "dbPassword" aynı sonucu verir.
func IsSensitiveKey(name string) bool {
	norm := normalizeKey(name)

	for _, frag := range sensitiveFragments {
		if strings.Contains(norm, frag) {
			return true
		}
	}
	for _, part := range strings.Split(norm, "_") {
		for _, exact := range sensitiveExact {
			if part == exact {
				return true
			}
		}
	}
	return false
}

// normalizeKey, anahtarı küçük harfe indirir, camelCase sınırlarını alt
// çizgiye çevirir ve tireyi alt çizgiyle birleştirir.
func normalizeKey(name string) string {
	var b strings.Builder
	b.Grow(len(name) + 4)

	runes := []rune(name)
	for i, r := range runes {
		switch {
		case r == '-' || r == '.' || r == ' ':
			b.WriteByte('_')
		case r >= 'A' && r <= 'Z':
			// camelCase sınırı: önceki karakter küçük harf veya rakamsa ayır.
			if i > 0 && !isUpper(runes[i-1]) && runes[i-1] != '_' {
				b.WriteByte('_')
			}
			b.WriteRune(r + ('a' - 'A'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

// RedactEnv, konteyner ortam değişkenlerini denetime yazılabilir hâle
// getirir: HER değer redakte edilir, yalnızca anahtar adları kalır.
//
// # Neden anahtar adına bakılmıyor?
//
// Ortam değişkenleri kullanıcının uygulamasına aittir ve adlandırmasını
// kullanıcı seçer. `IsSensitiveKey` sezgiseldir; `SMTP_URL` veya `CONFIG`
// gibi masum görünen bir adın altında bir parola durabilir. Bu yüzden
// burada sezgisel kullanılmaz — varsayılan REDDET uygulanır.
//
// Anahtar adlarının kalması denetim değeri taşır: "hangi değişkenler
// ayarlandı" sorusu yanıtlanabilir kalır, "değerleri neydi" sorusu ise
// zaten denetim günlüğünün yanıtlaması gereken bir soru değildir.
//
// nil harita için nil döner; boş harita için boş harita.
func RedactEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k := range env {
		out[k] = Redacted
	}
	return out
}

// RedactSensitive, karışık parametre haritasını denetime yazılabilir hâle
// getirir: yalnızca adı sır ima eden anahtarların değerleri redakte edilir.
//
// Ortam değişkenleri için bunu DEĞİL, RedactEnv'i kullanın.
func RedactSensitive(params map[string]string) map[string]string {
	if params == nil {
		return nil
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		if IsSensitiveKey(k) {
			out[k] = Redacted
			continue
		}
		out[k] = v
	}
	return out
}

// MarshalParams, Record.ParamsJSON alanı için JSON üretir.
//
// encoding/json harita anahtarlarını sıralar, bu yüzden çıktı
// belirlenimcidir — aynı girdi her zaman aynı baytları verir. Bu, zincirin
// yeniden hesaplanabilmesi için gereklidir.
//
// Kodlama başarısız olamaz (girdi map[string]string'tir); yine de sessizce
// boş JSON dönmek yerine hatayı görünür kılıyoruz.
func MarshalParams(m map[string]string) (string, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("audit: parametreler kodlanamadı: %w", err)
	}
	return string(b), nil
}

// SensitiveKeysIn, haritadaki sır ima eden anahtarları sıralı döndürür.
// Testler ve tanılama içindir.
func SensitiveKeysIn(m map[string]string) []string {
	var out []string
	for k := range m {
		if IsSensitiveKey(k) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

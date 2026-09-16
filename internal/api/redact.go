package api

import (
	"strings"

	"github.com/erkanrzgc/panely/internal/audit"
)

// ════════════════════════════════════════════════════════════════════
// KARIŞIK PARAMETRE REDAKSİYONU — yalnızca panelyd'nin politikası
// ════════════════════════════════════════════════════════════════════
//
// ── Neden internal/audit'te DEĞİL? ──────────────────────────────────
//
// Bu kod bir dönem `internal/audit` içindeydi ve oradan `panely-exec`'in
// içe aktarma grafiğine giriyordu. Ayrıcalıklı ikili onu HİÇ ÇAĞIRMIYOR
// — executor denetime yalnızca `audit.RedactEnv` (varsayılan REDDET) ile
// yazıyor. Ölçüldü: `deadcode ./cmd/panely-exec` bu dört fonksiyonu
// "unreachable" olarak işaretledi.
//
// Kaynak satırları yine de ayrıcalıklı yüzey bütçesine sayılıyordu (betik
// içe aktarılan paketlerin dosyalarını sayıyor, bağlayıcının eleyip
// elemediğine bakmıyor). Yani bütçe, root'un çalıştırmadığı 50 satırı
// root'a yazıyordu. Taşıma bir muhasebe oyunu değil, ÖLÇÜMÜ GERÇEĞE
// YAKLAŞTIRMA: kod artık gerçekten onu çalıştıran ikilinin yanında.
//
// ── Yan fayda: yanlış fonksiyonu seçmek zorlaştı ────────────────────
//
// `RedactSensitive` sezgiseldir ve ortam değişkenleri için YANLIŞ seçim
// (bir sır `CONFIG` adının altında durabilir). Eskiden ikisi yan yana
// dışa açıktı ve doğru olanı seçmek yorumu okumaya bağlıydı. Artık bu
// taraf dışa açık değil; `audit` paketinden env için gelen tek seçenek
// `RedactEnv`.

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

// isSensitiveKey, anahtar adının sır ima edip etmediğini söyler.
//
// Karşılaştırma büyük/küçük harf duyarsızdır ve ayırıcılar normalize
// edilir: "DB_PASSWORD", "db-password" ve "dbPassword" aynı sonucu verir.
func isSensitiveKey(name string) bool {
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

// redactSensitive, karışık parametre haritasını denetime yazılabilir hâle
// getirir: yalnızca adı sır ima eden anahtarların değerleri redakte edilir.
//
// ⚠ Ortam değişkenleri için bunu DEĞİL, audit.RedactEnv'i kullanın.
// Sezgisel, kullanıcının seçtiği adlarda güvenilmez: bir parola `CONFIG`
// ya da `SMTP_URL` adının altında durabilir. Env'de varsayılan REDDET
// uygulanır, burada varsayılan GEÇİR — çünkü buradaki anahtarları panely
// KENDİSİ üretiyor ve hangilerinin sır olabileceğini biliyor.
func redactSensitive(params map[string]string) map[string]string {
	if params == nil {
		return nil
	}
	out := make(map[string]string, len(params))
	for k, v := range params {
		if isSensitiveKey(k) {
			out[k] = audit.Redacted
			continue
		}
		out[k] = v
	}
	return out
}

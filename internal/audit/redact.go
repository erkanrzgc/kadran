package audit

import (
	"encoding/json"
	"fmt"
)

// Redacted, denetim kaydında bir değerin yerine yazılan işarettir.
//
// Değerin UZUNLUĞU da sızdırılmaz: "hunter2" ve 64 baytlık bir API
// anahtarı aynı işarete dönüşür. Uzunluk tek başına bile bilgidir.
const Redacted = "[REDACTED]"

// RedactEnv, konteyner ortam değişkenlerini denetime yazılabilir hâle
// getirir: HER değer redakte edilir, yalnızca anahtar adları kalır.
//
// # Neden anahtar adına bakılmıyor?
//
// Ortam değişkenleri kullanıcının uygulamasına aittir ve adlandırmasını
// kullanıcı seçer. Ad sezgiseli burada güvenilmez: `SMTP_URL` veya
// `CONFIG` gibi masum görünen bir adın altında bir parola durabilir. Bu
// yüzden varsayılan REDDET uygulanır.
//
// Anahtar adlarının kalması denetim değeri taşır: "hangi değişkenler
// ayarlandı" sorusu yanıtlanabilir kalır, "değerleri neydi" sorusu ise
// zaten denetim günlüğünün yanıtlaması gereken bir soru değildir.
//
// ⚠ Karışık parametre haritaları (panely'nin KENDİ ürettiği anahtarlar)
// için ayrı bir politika var ve o KASITLI olarak bu pakette değil:
// `internal/api` içindeki `redactSensitive`. Gerekçesi orada yazılı —
// ayrıcalıklı ikili onu hiç çağırmıyor, bu yüzden ayrıcalıklı yüzeyde
// durmasının bir sebebi yoktu.
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

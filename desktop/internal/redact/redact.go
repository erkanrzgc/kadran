// Package redact, sunucudan ya da dağıtıcıdan gelen hatanın arayüze
// gidecek metninin TEK kurucusu.
//
// Dağıtıcı hatayı `{message: "call failed" | "could not connect", data:
// "<gerçek sebep>"}` biçiminde döndürür; yalnız message'ı göstermek
// kullanıcıya "call failed" demekti. Burada ikisi birleştirilir, sır
// sızdırabilecek parçalar silinir ve uzunluk sınırlanır. Arayüze HAM data
// hiç gitmez.
//
// Sıra önemli: önce kontrol/biçim karakterleri atılır, SONRA sırlar
// silinir. Tersi olsaydı `ghp_` içine sıfır genişlikli karakter sokan bir
// metin redaksiyonu atlatırdı.
package redact

import (
	"regexp"
)

// MaxErrorChars, kullanıcıya giden metnin en fazla kod noktası sayısı
// (sonuna "…" eklenir).
const MaxErrorChars = 500

const redacted = "***"

var (
	lineBreaks = regexp.MustCompile(`[\r\n\t]+`)
	unsafeRune = regexp.MustCompile(`[\p{Cc}\p{Cf}]`)
)

// jsSpace, JavaScript düzenli ifadesindeki \s: kurallar Electron'dakiyle
// aynı yerde dursun (Go'nun \s'i yalnız ASCII boşluğu).
const jsSpace = `\s\x{0b}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// Her kural adlı bir değişken: mutasyon ölçümü tek tek devre dışı bırakır.
var (
	xAccessToken = regexp.MustCompile(`(?i)x-access-token:[^@` + jsSpace + `/]*@`)
	urlUserinfo  = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^/` + jsSpace + `]*@`)
	githubPAT    = regexp.MustCompile(`github_pat_[A-Za-z0-9_]+`)
	githubToken  = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]+`)
)

// stripUnsafe: satır sonu ve sekme boşluğa çevrilir; geri kalan kontrol
// (Cc) ve biçim (Cf: bidi, sıfır genişlik) karakterleri atılır.
func stripUnsafe(text string) string {
	return unsafeRune.ReplaceAllString(lineBreaks.ReplaceAllString(text, " "), "")
}

// Secrets, bağlantı adreslerindeki kimlik bilgisini ve GitHub jetonlarını siler.
func Secrets(text string) string {
	text = xAccessToken.ReplaceAllString(text, "x-access-token:"+redacted+"@")
	text = urlUserinfo.ReplaceAllString(text, "${1}"+redacted+"@")
	text = githubPAT.ReplaceAllString(text, redacted)
	return githubToken.ReplaceAllString(text, redacted)
}

func limit(text string) string {
	runes := []rune(text)
	if len(runes) <= MaxErrorChars {
		return text
	}
	return string(runes[:MaxErrorChars]) + "…"
}

// Text, hatanın kullanıcıya gidecek güvenli metni: message + (varsa) data.
func Text(message, data string) string {
	raw := message
	if data != "" {
		raw = message + ": " + data
	}
	return limit(Secrets(stripUnsafe(raw)))
}

// Error, düz bir hatanın arıtılmış metni.
func Error(err error) string {
	if err == nil {
		return "unknown error"
	}
	return Text(err.Error(), "")
}

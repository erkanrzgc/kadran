// Package guard, arayüzden gelen her girdinin doğrulanması.
//
// Arayüz (WebView) güvenilmez bir kaynaktır: bir XSS ya da kötü bir
// bağımlılık oradan istediği değeri gönderebilir. Go tarafı her değeri
// sunucuya ya da onay kutusuna ulaşmadan burada keser. Kurallar Electron
// ana sürecindekilerin aynısı (K-165); hata iletileri İngilizce, arayüze
// ulaşıyorlar (K-141).
package guard

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// utf16Len, metnin JavaScript'teki uzunluğu (UTF-16 kod birimi). Sınırlar
// Electron'da bu ölçüyle konmuştu; aynı metin aynı yerde reddedilsin.
// Geçersiz bayt U+FFFD olarak tek birim sayılır.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// runeCount, metnin kod noktası sayısı (JavaScript'te [...s].length).
func runeCount(s string) int { return utf8.RuneCountInString(s) }

// jsSpace, JavaScript'in String.prototype.trim'inin attığı karakterler.
func jsSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', '\u00a0', '\u1680', '\u2028', '\u2029', '\u202f', '\u205f', '\u3000', '\ufeff':
		return true
	}
	return r >= '\u2000' && r <= '\u200a'
}

// jsTrim, JavaScript'teki trim(): Go'nun TrimSpace'inden farklı olarak
// U+0085'i bırakır, U+FEFF'i atar.
func jsTrim(s string) string { return strings.TrimFunc(s, jsSpace) }

// Package appearance, kullanıcı ayarları (dil, tema, yazı tipleri) ve
// pencerenin yerel görünümü. Tema iki tanedir: açık ve koyu. Cam ayrı bir
// tema değil, her temada çerçevede (K-163).
//
// Dosya elle düzenlenebilir ya da bozulabilir. Her alan TEK TEK doğrulanır;
// geçersiz alan varsayılana döner, geçerli alanlar korunur. Kurallar
// arayüzdeki src/shared/appearance.ts ile aynı.
package appearance

import (
	"encoding/json"
	"math"
	"slices"
	"unicode/utf16"
)

// Version, ayar dosyasının biçim sürümü.
const Version = 2

// MaxFontFamily, yazı tipi adının en fazla uzunluğu (UTF-16 birimi).
const MaxFontFamily = 200

var (
	languages = []string{"en", "tr"}
	themes    = []string{"dark", "light"}
)

// Font, seçilen yazı tipi; boş aile gömülü Geist demek.
type Font struct {
	Family string `json:"family"`
	Weight int    `json:"weight"`
}

// Appearance, kayıtlı ayarlar. Alan sırası dosyadaki sıradır.
type Appearance struct {
	Version  int    `json:"version"`
	Language string `json:"language"`
	Theme    string `json:"theme"`
	UIFont   Font   `json:"uiFont"`
	CodeFont Font   `json:"codeFont"`
}

// Defaults, varsayılan ayarlar: İngilizce, açık tema, gömülü yazı tipleri.
func Defaults() Appearance {
	return Appearance{
		Version:  Version,
		Language: "en",
		Theme:    "light",
		UIFont:   Font{Family: "", Weight: 400},
		CodeFont: Font{Family: "", Weight: 400},
	}
}

func units(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func pick(v any, allowed []string, fallback string) string {
	if s, ok := v.(string); ok && slices.Contains(allowed, s) {
		return s
	}
	return fallback
}

// clampWeight: JavaScript'teki Math.round ile yuvarlar (.5 yukarı), 100–900.
func clampWeight(v any, fallback int) int {
	f, ok := v.(float64)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return fallback
	}
	r := math.Floor(f + 0.5)
	return int(math.Min(900, math.Max(100, r)))
}

func font(v any, fallback Font) Font {
	o, ok := v.(map[string]any)
	if !ok {
		return fallback
	}
	family := fallback.Family
	if s, isText := o["family"].(string); isText && units(s) <= MaxFontFamily {
		family = s
	}
	return Font{Family: family, Weight: clampWeight(o["weight"], fallback.Weight)}
}

// Sanitize, güvenilmez bir JSON değerinden geçerli ayarlar üretir. Eski
// sürümün dosyasından yalnız dil taşınır; kaldırılan cam teması ("glass")
// bilinmeyen değer gibi varsayılana (açık) döner.
func Sanitize(raw json.RawMessage) Appearance {
	d := Defaults()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return d
	}
	o, ok := v.(map[string]any)
	if !ok {
		return d
	}
	language := pick(o["language"], languages, d.Language)
	if version, isNumber := o["version"].(float64); !isNumber || version != Version {
		d.Language = language
		return d
	}
	return Appearance{
		Version:  Version,
		Language: language,
		Theme:    pick(o["theme"], themes, d.Theme),
		UIFont:   font(o["uiFont"], d.UIFont),
		CodeFont: font(o["codeFont"], d.CodeFont),
	}
}

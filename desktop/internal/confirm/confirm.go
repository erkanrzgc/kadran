package confirm

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
)

// Kind, işlemin türü.
type Kind string

const (
	KindCreate   Kind = "create"
	KindUpdate   Kind = "update"
	KindDeploy   Kind = "deploy"
	KindDelete   Kind = "delete"
	KindRollback Kind = "rollback"
	KindBackup   Kind = "backup"
)

// Request, onaylanacak işlem. Spec yalnız create'te, Update yalnız
// update'te, Ref yalnız deploy'da dolu.
type Request struct {
	Kind   Kind
	Target string
	App    string
	Ref    string
	Spec   *jsonv.Object
	Update *jsonv.Object
}

// Box, yerel onay kutusunun metinleri. Varsayılan düğme her zaman Cancel.
type Box struct {
	Title   string
	Message string
	Detail  string
	Cancel  string
	Action  string
}

const (
	maxValueChars = 200
	maxListItems  = 10
	ellipsis      = 0x2026
)

// blankLooking: harf sayılan ama boş görünen karakterler (Hangul dolgu,
// Braille boşluğu) ve değerin içindeki "…" (yalnız gerçek kesme "…"
// göstersin).
var blankLooking = map[rune]bool{0x115f: true, 0x1160: true, 0x3164: true, 0xffa0: true, 0x2800: true, ellipsis: true}

// invisible: kutuda görünür kaçışa çevrilenler. Denetim, biçim (bidi dahil),
// vekil, özel kullanım, atanmamış, birleşik işaret, satır/paragraf ayırıcı,
// düz boşluk dışındaki boşluklar ve blankLooking.
func invisible(r rune) bool {
	switch {
	case blankLooking[r]:
		return true
	case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Cs, unicode.Co, unicode.Cn, unicode.M, unicode.Zl, unicode.Zp):
		return true
	case unicode.Is(unicode.Zs, r):
		return r != ' '
	}
	return false
}

// units, metnin JavaScript'teki uzunluğu (UTF-16 kod birimi); sınır
// Electron'daki kutuyla aynı ölçülsün.
func units(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

// safe, görünmez karakterleri görünür kaçışa çevirir, SONRA uzunu ORTADAN
// keser. Önce kesilseydi 200 karakter kaçışla ~1200'e büyürdü; baştan
// kesilseydi `user@host` hedefinin host'u (kuyruk) kutuda görünmezdi.
func safe(v any) string {
	s, _ := v.(string)
	var parts []string
	for _, r := range s {
		if invisible(r) {
			parts = append(parts, fmt.Sprintf("%cu%04x", '\\', r))
		} else {
			parts = append(parts, string(r))
		}
	}
	whole := strings.Join(parts, "")
	if units(whole) <= maxValueChars {
		return whole
	}
	half := maxValueChars / 2
	var head, tail strings.Builder
	headLen, i := 0, 0
	for i < len(parts) && headLen+units(parts[i]) <= half {
		head.WriteString(parts[i])
		headLen += units(parts[i])
		i++
	}
	tailParts, tailLen := []string{}, 0
	for j := len(parts) - 1; j >= i && tailLen+units(parts[j]) <= half; j-- {
		tailParts = append([]string{parts[j]}, tailParts...)
		tailLen += units(parts[j])
	}
	for _, p := range tailParts {
		tail.WriteString(p)
	}
	return head.String() + string(rune(ellipsis)) + tail.String()
}

var decimalText20 = regexp.MustCompile(`^\d{1,20}$`)

// decimal: doğrulayıcının kabul ettiği İKİ sayı biçimi (JSON sayısı ya da
// onluk metin, protojson gibi) kanonik değere çevrilir. Kutu yalnız birini
// tanısaydı öbür biçimde gelen değer boş/farklı görünür, onaylanan ile
// gönderilen ayrışırdı.
func decimal(v any) *big.Int {
	switch t := v.(type) {
	case json.Number:
		f, err := strconv.ParseFloat(string(t), 64)
		if err == nil && f >= 0 && f == math.Trunc(f) && f <= 1<<53-1 {
			return big.NewInt(int64(f))
		}
	case string:
		if decimalText20.MatchString(t) {
			n, _ := new(big.Int).SetString(t, 10)
			return n
		}
	}
	return nil
}

// jsString, JavaScript'teki String(v); yalnız beklenmeyen biçimlerde kullanılır.
func jsString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case json.Number:
		return string(t)
	case []any:
		items := make([]string, len(t))
		for i, item := range t {
			items[i] = jsString(item)
		}
		return strings.Join(items, ",")
	}
	return "[object Object]"
}

func count(v any) string {
	if n := decimal(v); n != nil {
		return n.String()
	}
	return safe(jsString(v))
}

var mib = big.NewInt(1024 * 1024)

// memory: bayt sayısı → MiB'e tam bölünüyorsa "8192 MiB", değilse
// yuvarlamadan bayt.
func memory(v any) string {
	n := decimal(v)
	if n == nil {
		return safe(jsString(v))
	}
	if n.Sign() > 0 && new(big.Int).Mod(n, mib).Sign() == 0 {
		return new(big.Int).Div(n, mib).String() + " MiB"
	}
	return n.String() + " B"
}

func get(o *jsonv.Object, key string) any {
	if o == nil {
		return nil
	}
	v, _ := o.Get(key)
	return v
}

func record(v any) *jsonv.Object {
	if o, ok := v.(*jsonv.Object); ok {
		return o
	}
	return jsonv.NewObject()
}

func list(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

// truthy: JavaScript'te boş olmayan metin.
func truthy(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

package console

import "strings"

const redacted = "***"

// maskedFlags, değeri arıtılan bayraklar: ikisi de KEY=değer biçiminde sır
// taşır. Geçmişten geri çağrılan *** değerini reddeden denetim (R9) de bu
// listeye bakar.
var maskedFlags = []string{"env", "build-arg"}

// Redact, argümanları insanın göreceği tek satıra çevirir (geçmiş ve onay
// kutusu): maskedFlags değerleri, Go'nun flag paketinin kabul ettiği her
// yazımda (-ad ya da --ad; ayrı ya da "=" ile; tekrarlı) KEY=*** olur, "="sız
// değer *** olur. Boşluk ya da tırnak içeren sözcük çift tırnağa alınır;
// içindeki " ve \ ters bölüyle kaçırılır (yalnız gösterim içindir).
func Redact(args []string) string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, joined := strings.Cut(a, "=")
		switch {
		case isMaskedFlag(a) && i+1 < len(args):
			out = append(out, quote(a), quote(maskEnv(args[i+1])))
			i++
		case joined && isMaskedFlag(name):
			out = append(out, quote(name+"="+maskEnv(value)))
		default:
			out = append(out, quote(a))
		}
	}
	return strings.Join(out, " ")
}

// isMaskedFlag: a, maskedFlags'tan birinin tek ya da çift tireli adı.
func isMaskedFlag(a string) bool {
	for _, f := range maskedFlags {
		if a == "-"+f || a == "--"+f {
			return true
		}
	}
	return false
}

// maskEnv, "KEY=değer" için KEY=***, "="sız değer için *** döner.
func maskEnv(v string) string {
	if k, _, ok := strings.Cut(v, "="); ok {
		return k + "=" + redacted
	}
	return redacted
}

func quote(w string) string {
	if w != "" && !strings.ContainsAny(w, " \t\"'") {
		return w
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(w) + `"`
}

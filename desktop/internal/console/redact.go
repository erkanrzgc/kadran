package console

import "strings"

const redacted = "***"

// Redact, argümanları insanın göreceği tek satıra çevirir (geçmiş ve onay
// kutusu): -env/--env değerleri (ayrı ya da "=" biçiminde, tekrarlı) KEY=***
// olur, "="sız değer *** olur. Boşluk ya da tırnak içeren sözcük çift tırnağa
// alınır; içindeki " ve \ ters bölüyle kaçırılır (yalnız gösterim içindir).
func Redact(args []string) string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case isEnvFlag(a) && i+1 < len(args):
			out = append(out, quote(a), quote(maskEnv(args[i+1])))
			i++
		case strings.HasPrefix(a, "-env="), strings.HasPrefix(a, "--env="):
			eq := strings.IndexByte(a, '=')
			out = append(out, quote(a[:eq+1]+maskEnv(a[eq+1:])))
		default:
			out = append(out, quote(a))
		}
	}
	return strings.Join(out, " ")
}

func isEnvFlag(a string) bool { return a == "-env" || a == "--env" }

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

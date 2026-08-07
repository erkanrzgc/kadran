package main

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// ── Boyut ayrıştırma ─────────────────────────────────────────────────

// sizeUnits, İKİLİ öneklerdir (Ki = 1024), ondalık değil.
//
// Bellek limiti cgroups'a bayt olarak gider ve orada "512M" her zaman
// 512 MiB anlamına gelir. Ondalık yorumlamak (512 × 10⁶) limiti sessizce
// %5 küçültürdü — ve bir bellek limitindeki %5, OOM ile çalışan bir
// konteyner arasındaki fark olabilir.
var sizeUnits = []struct {
	suffix string
	mult   uint64
}{
	{"Gi", 1 << 30},
	{"Mi", 1 << 20},
	{"Ki", 1 << 10},
	{"G", 1 << 30},
	{"M", 1 << 20},
	{"K", 1 << 10},
	{"B", 1},
}

// parseSize, "512Mi" gibi bir boyutu bayta çevirir.
func parseSize(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("boyut boş olamaz")
	}

	for _, u := range sizeUnits {
		digits, ok := strings.CutSuffix(s, u.suffix)
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(digits), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("boyut çözümlenemedi (%q)", s)
		}
		if n == 0 {
			return 0, fmt.Errorf("boyut sıfır olamaz (%q) — limitsiz konteyner yoktur", s)
		}
		// Taşma denetimi: 4Ei üstü değerler sessizce sarmalanırdı ve
		// "çok büyük limit" bir anda "çok küçük limit" olurdu.
		if n > ^uint64(0)/u.mult {
			return 0, fmt.Errorf("boyut çok büyük (%q)", s)
		}
		return n * u.mult, nil
	}

	// Soneksiz değer ham bayt sayılır.
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("boyut çözümlenemedi (%q) — ör. 512Mi, 2Gi, 1073741824", s)
	}
	if n == 0 {
		return 0, fmt.Errorf("boyut sıfır olamaz — limitsiz konteyner yoktur")
	}
	return n, nil
}

// formatSize, baytı okunabilir bir ikili boyuta çevirir.
func formatSize(b uint64) string {
	switch {
	case b >= 1<<30 && b%(1<<30) == 0:
		return strconv.FormatUint(b>>30, 10) + "Gi"
	case b >= 1<<20 && b%(1<<20) == 0:
		return strconv.FormatUint(b>>20, 10) + "Mi"
	case b >= 1<<10 && b%(1<<10) == 0:
		return strconv.FormatUint(b>>10, 10) + "Ki"
	default:
		return strconv.FormatUint(b, 10) + "B"
	}
}

// ── Tekrarlanabilir ANAHTAR=DEĞER bayrağı ────────────────────────────

type stringMap map[string]string

func (m stringMap) String() string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ",")
}

func (m stringMap) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || k == "" {
		return fmt.Errorf("ANAHTAR=DEĞER bekleniyordu (%q)", v)
	}
	if _, dup := m[k]; dup {
		// Sessizce üzerine yazmak, iki kez verilen bir argümanın hangi
		// değerinin geçerli olduğunu belirsiz bırakırdı.
		return fmt.Errorf("%q birden çok kez verildi", k)
	}
	m[k] = val
	return nil
}

// stringMapFlag, tekrarlanabilir bir ANAHTAR=DEĞER bayrağı tanımlar.
func (c *cli) stringMapFlag(fs *flag.FlagSet, name, usage string) *map[string]string {
	m := stringMap{}
	fs.Var(m, name, usage)
	out := map[string]string(m)
	return &out
}

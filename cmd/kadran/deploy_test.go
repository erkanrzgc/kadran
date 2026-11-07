package main

import (
	"testing"
)

// ── splitRepo ────────────────────────────────────────────────────────

func TestSplitRepo(t *testing.T) {
	host, owner, name, err := splitRepo("github.com/erkanrzgc/kadran")
	if err != nil {
		t.Fatalf("ayrıştırılamadı: %v", err)
	}
	if host != "github.com" || owner != "erkanrzgc" || name != "kadran" {
		t.Errorf("üçlü = %q/%q/%q", host, owner, name)
	}

	// ".git" soneki kırpılır: kopyala-yapıştır alışkanlığı.
	if _, _, name, err = splitRepo("github.com/erkanrzgc/kadran.git"); err != nil || name != "kadran" {
		t.Errorf(".git soneki kırpılmadı: %q (%v)", name, err)
	}
}

// TestSplitRepoRejectsURLs, şemalı girdinin SESSİZCE kırpılmadığını
// doğrular.
//
// Bu tasarımda hiçbir yerde URL ALINMIYOR — bağlam URL'ini executor
// kendisi kuruyor. Şemayı sessizce atmak, o sınırı bulanık gösterir ve
// kullanıcıyı "buraya URL verebiliyorum" sanısına iterdi.
func TestSplitRepoRejectsURLs(t *testing.T) {
	cases := []string{
		"https://github.com/user/repo",
		"git://github.com/user/repo",
		"ssh://git@github.com/user/repo",
	}
	for _, in := range cases {
		if _, _, _, err := splitRepo(in); err == nil {
			t.Errorf("URL kabul edildi: %q", in)
		}
	}
}

func TestSplitRepoRejectsWrongShape(t *testing.T) {
	for _, in := range []string{"", "github.com", "github.com/user", "a/b/c/d"} {
		if _, _, _, err := splitRepo(in); err == nil {
			t.Errorf("biçimsiz değer kabul edildi: %q", in)
		}
	}
}

// ── parseSize ────────────────────────────────────────────────────────

// TestParseSizeUsesBinaryUnits, "M" ekinin MiB anlamına geldiğini
// doğrular.
//
// Ondalık yorumlamak (512 × 10⁶) limiti sessizce ~%5 küçültürdü ve bir
// bellek limitindeki %5, OOM ile çalışan konteyner arasındaki fark
// olabilir. cgroups'ta bu değer her zaman ikili.
func TestParseSizeUsesBinaryUnits(t *testing.T) {
	cases := map[string]uint64{
		"512Mi":      512 << 20,
		"512M":       512 << 20,
		"2Gi":        2 << 30,
		"2G":         2 << 30,
		"1024Ki":     1 << 20,
		"1073741824": 1 << 30,
		"4096B":      4096,
	}
	for in, want := range cases {
		got, err := parseSize(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q = %d, beklenen %d", in, got, want)
		}
	}
}

func TestParseSizeRejectsZeroAndGarbage(t *testing.T) {
	for _, in := range []string{"", "0", "0Mi", "abc", "-1", "12Xi", "1.5Gi"} {
		if v, err := parseSize(in); err == nil {
			t.Errorf("%q kabul edildi (%d) — limitsiz konteyner yoktur", in, v)
		}
	}
}

// TestParseSizeRejectsOverflow, taşmanın YAKALANDIĞINI doğrular.
//
// Denetim olmasaydı çarpım sessizce sarmalanır ve "çok büyük limit" bir
// anda "çok küçük limit" olurdu — kullanıcının istediğinin tam tersi.
func TestParseSizeRejectsOverflow(t *testing.T) {
	if v, err := parseSize("99999999999Gi"); err == nil {
		t.Errorf("taşan boyut kabul edildi: %d", v)
	}
}

func TestFormatSizeRoundTrips(t *testing.T) {
	for _, in := range []string{"512Mi", "2Gi", "4Ki"} {
		n, err := parseSize(in)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got := formatSize(n); got != in {
			t.Errorf("formatSize(parseSize(%q)) = %q", in, got)
		}
	}
}

// ── ANAHTAR=DEĞER bayrağı ────────────────────────────────────────────

func TestStringMapRejectsDuplicates(t *testing.T) {
	m := stringMap{}
	if err := m.Set("A=1"); err != nil {
		t.Fatalf("ilk atama: %v", err)
	}
	// Sessizce üzerine yazmak, hangi değerin geçerli olduğunu belirsiz
	// bırakırdı.
	if err := m.Set("A=2"); err == nil {
		t.Error("aynı anahtar iki kez kabul edildi")
	}
	if m["A"] != "1" {
		t.Errorf("değer değişti: %q", m["A"])
	}
}

func TestStringMapRequiresEquals(t *testing.T) {
	m := stringMap{}
	for _, in := range []string{"NOEQUALS", "=deger"} {
		if err := m.Set(in); err == nil {
			t.Errorf("biçimsiz değer kabul edildi: %q", in)
		}
	}
	// Boş DEĞER geçerlidir: `-build-arg DEBUG=` bilinçli bir kullanım.
	if err := m.Set("DEBUG="); err != nil {
		t.Errorf("boş değer reddedildi: %v", err)
	}
}

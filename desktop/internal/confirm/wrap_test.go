package confirm

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// wantWordUnits, kararın değeri; sabitten bağımsız, böylece sabit değişirse
// test düşer (mutant 32 yerine 96 yazarsa).
const wantWordUnits = 32

// longestWord, Detail içindeki en uzun boşluksuz koşu (UTF-16 birimi).
func longestWord(detail string) int {
	longest := 0
	for _, row := range strings.Split(detail, "\n") {
		for _, word := range strings.Fields(row) {
			longest = max(longest, units(word))
		}
	}
	return longest
}

var (
	wholeEscape = regexp.MustCompile(`\\u[0-9a-f]{4}`)
	anyEscape   = regexp.MustCompile(`\\u`)
)

// checkWrapped, bir komut satırının kutusunun her değişmezini dener: satır
// 96 birimi, koşu 32 birimi aşmaz; her satır geçerli UTF-8; kaçışlar bütün;
// girinti atılıp birleşince özgün (kaçışlı) satır bire bir geri gelir.
func checkWrapped(t *testing.T, name string, lang Language, line string) {
	t.Helper()
	box := BuildConsole(lang, ConsoleRequest{Line: line})
	if got := longestWord(box.Detail); got > wantWordUnits {
		t.Fatalf("%s/%s: boşluksuz koşu %d birim > %d:\n%s", name, lang, got, wantWordUnits, box.Detail)
	}
	for i, row := range strings.Split(box.Detail, "\n") {
		if units(row) > maxRowUnits {
			t.Fatalf("%s/%s satır %d: %d birim > %d", name, lang, i, units(row), maxRowUnits)
		}
		if !utf8.ValidString(row) {
			t.Fatalf("%s/%s satır %d geçersiz UTF-8: %q", name, lang, i, row)
		}
		if len(anyEscape.FindAllString(row, -1)) != len(wholeEscape.FindAllString(row, -1)) {
			t.Fatalf("%s/%s satır %d: kaçış satırlar arasında bölündü: %q", name, lang, i, row)
		}
	}
	if !strings.Contains(unwrap(box.Detail), strings.Join(escapeParts(line), "")) {
		t.Fatalf("%s/%s: birleşen satır özgün değil:\n%s", name, lang, box.Detail)
	}
}

// Canlı bulgu (22 Kas): 96 birimlik W koşusu TaskDialog içinde "…" ile
// kısaltıldı. Boşluksuz hiçbir koşu wantWordUnits birimden uzun olmaz ve
// satırlar birleşince özgün satır bire bir geri gelir.
func TestBuildConsoleBreaksLongWords(t *testing.T) {
	lines := map[string]string{
		"W":          "app update -skip-dns-check -domain=" + strings.Repeat("W", 88) + ".example.com ozel-deneme",
		"full width": "app update -domain=" + strings.Repeat("Ｗ", 100) + ".example.com x",
		"emoji":      "app update -name " + strings.Repeat(cp(0x1F600), 50) + " web",
		"escape":     "app update -name " + strings.Repeat(cp(0x202e), 20) + " web",
		"plain":      strings.Repeat("a", 200),
	}
	for name, line := range lines {
		for _, lang := range []Language{English, Turkish} {
			checkWrapped(t, name, lang, line)
		}
	}
	// Kısa sözcüklü boşluklu satır eskisi gibi tek satırda kalır.
	box := BuildConsole(English, ConsoleRequest{Line: strings.TrimSpace(strings.Repeat("word ", 17))})
	if strings.Contains(box.Detail, "\n") {
		t.Fatalf("kısa boşluklu satır bölündü:\n%s", box.Detail)
	}
}

// Boşluklu metin satırı ~90 birime doldurur, ardından uzun boşluksuz koşu
// gelir: satır sonundaki kısa koşu bir sonraki satıra taşınır ve taşınan
// kısım koşuya SAYILMALI (yoksa taşınan + 32 birim ≈ 63'lük koşu çıkar).
func TestBuildConsoleCountsCarriedRun(t *testing.T) {
	for _, lang := range []Language{English, Turkish} {
		for tail := 1; tail <= 31; tail++ {
			line := strings.Repeat("aaaaaaaa ", 9) + strings.Repeat("b", tail) + strings.Repeat("b", 70)
			checkWrapped(t, "carry", lang, line)
		}
	}
}

// Kaçış (6 birim) 32 birimlik sınırda ortadan bölünmez: 5 + 4 kaçış = 29
// birim, 5. kaçış 35'e çıkar; kaçış bütün olarak yeni satıra geçer.
func TestBuildConsoleNeverSplitsEscapeAtWordLimit(t *testing.T) {
	esc := cp(0x202e)
	for lead := 0; lead <= 12; lead++ {
		line := strings.Repeat("a", lead) + strings.Repeat(esc, 12) + strings.Repeat("a", 7)
		for _, lang := range []Language{English, Turkish} {
			checkWrapped(t, "escape-boundary", lang, line)
		}
	}
}

// Vekil çift (emoji) 32 birim sınırında ortadan bölünmez ve koşu gerçekten
// bölünür: bölme kapalıysa 80 birimlik koşu kalır ve test düşer.
func TestBuildConsoleKeepsSurrogatePairsWhole(t *testing.T) {
	line := strings.Repeat(cp(0x1F600), 40)
	checkWrapped(t, "surrogate", English, line)
	box := BuildConsole(English, ConsoleRequest{Line: line})
	if got := strings.Count(unwrap(box.Detail), cp(0x1F600)); got != 40 {
		t.Fatalf("%d emoji, 40 olmalı:\n%s", got, box.Detail)
	}
}

// Sabit tohumlu özellik testi: rastgele karışık satırlarda değişmezler tutar.
func TestBuildConsoleWrapInvariantsRandomised(t *testing.T) {
	pieces := []string{"a", "W", "Ｗ", " ", " ", cp(0x1F600), cp(0x202e), "-", "=", "/", ".example.com"}
	rng := rand.New(rand.NewSource(20261122))
	for n := 0; n < 400; n++ {
		var b strings.Builder
		for i, count := 0, 1+rng.Intn(300); i < count; i++ {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		line := b.String()
		if ConsoleLineUnits(line) > MaxConsoleLineUnits {
			continue
		}
		checkWrapped(t, "random", English, line)
	}
}

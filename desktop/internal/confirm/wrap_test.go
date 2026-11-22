package confirm

import (
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

// Canlı bulgu (22 Kas): 96 birimlik W koşusu TaskDialog içinde "…" ile
// kısaltıldı. Boşluksuz hiçbir koşu maxWordUnits birimden uzun olmaz ve
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
			box := BuildConsole(lang, ConsoleRequest{Server: "s", Line: line, Effect: "E."})
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
			}
			if !strings.Contains(unwrap(box.Detail), strings.Join(escapeParts(line), "")) {
				t.Fatalf("%s/%s: birleşen satır özgün değil:\n%s", name, lang, box.Detail)
			}
		}
	}
	// Kısa sözcüklü boşluklu satır eskisi gibi tek satırda kalır.
	box := BuildConsole(English, ConsoleRequest{Line: strings.TrimSpace(strings.Repeat("word ", 17))})
	if strings.Contains(box.Detail, "\n") {
		t.Fatalf("kısa boşluklu satır bölündü:\n%s", box.Detail)
	}
}

// Vekil çift (emoji) 32 birim sınırında ortadan bölünmez: her satır geçerli
// UTF-8 ve emoji sayısı korunur.
func TestBuildConsoleKeepsSurrogatePairsWhole(t *testing.T) {
	line := strings.Repeat(cp(0x1F600), 40)
	box := BuildConsole(English, ConsoleRequest{Line: line})
	if got := strings.Count(unwrap(box.Detail), cp(0x1F600)); got != 40 {
		t.Fatalf("%d emoji, 40 olmalı:\n%s", got, box.Detail)
	}
}

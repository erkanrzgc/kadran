package main

import (
	"strings"
	"testing"
	"unicode/utf8"

	"rsc.io/qr"
)

// ── Kurulum QR'ı (K-156) ─────────────────────────────────────────────

// geriOku, yarım blok çizimini modül satırlarına geri çevirir (true = açık).
// Her metin satırı iki modül satırı taşır.
func geriOku(t *testing.T, s string) [][]bool {
	t.Helper()
	var rows [][]bool
	for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		var top, bottom []bool
		for _, r := range line {
			switch r {
			case '█':
				top, bottom = append(top, true), append(bottom, true)
			case '▀':
				top, bottom = append(top, true), append(bottom, false)
			case '▄':
				top, bottom = append(top, false), append(bottom, true)
			case ' ':
				top, bottom = append(top, false), append(bottom, false)
			default:
				t.Fatalf("beklenmeyen karakter %q", r)
			}
		}
		rows = append(rows, top, bottom)
	}
	return rows
}

// TestQRBlockMatchesCode: çizim kodun modüllerini birebir taşıyor. Açık
// modüller blok, koyular boşluk (koyu zeminli terminal); çevrede 4 modül
// açık sessiz bölge; modül sayısı tek olduğu için son satırın alt yarısı
// boş (terminal zemini).
func TestQRBlockMatchesCode(t *testing.T) {
	const uri = "otpauth://totp/Kadran:srv.example?algorithm=SHA1&digits=6&issuer=Kadran&period=30&secret=JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"
	s, err := qrBlock(uri)
	if err != nil {
		t.Fatal(err)
	}
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		t.Fatal(err)
	}
	const quiet = 4
	n := code.Size + 2*quiet // QR kenarı hep tek: 17+4·sürüm
	got := geriOku(t, s)
	if len(got) != n+1 {
		t.Fatalf("%d modül satırı, beklenen %d ve bir boş yarım", len(got), n)
	}
	for y := 0; y < n; y++ {
		if len(got[y]) != n {
			t.Fatalf("satır %d: %d modül, beklenen %d", y, len(got[y]), n)
		}
		for x := 0; x < n; x++ {
			cx, cy := x-quiet, y-quiet
			want := cx < 0 || cy < 0 || cx >= code.Size || cy >= code.Size || !code.Black(cx, cy)
			if got[y][x] != want {
				t.Fatalf("modül (%d,%d): açık=%v, beklenen %v", x, y, got[y][x], want)
			}
		}
	}
	for x, light := range got[n] {
		if light {
			t.Fatalf("son yarım satır boş olmalı, %d. sütun dolu", x)
		}
	}
}

// TestQRBlockFitsTerminal: en uzun ana bilgisayar adıyla (64 karakter) bile
// QR 80 sütuna satır kaydırmadan sığıyor. Kaymış bir QR okunmaz.
func TestQRBlockFitsTerminal(t *testing.T) {
	uri := "otpauth://totp/Kadran:" + strings.Repeat("h", 64) +
		"?algorithm=SHA1&digits=6&issuer=Kadran&period=30&secret=" + strings.Repeat("A", 32)
	s, err := qrBlock(uri)
	if err != nil {
		t.Fatal(err)
	}
	for i, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if w := utf8.RuneCountInString(line); w > 80 {
			t.Fatalf("satır %d %d sütun", i, w)
		}
	}
}

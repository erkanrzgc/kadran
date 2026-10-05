package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// cliPackages, kullanıcının CLI'da gördüğü metni üreten paketler (K-141,
// 1. aşama). Sunucunun ürettiği metin (kurulum çıktısı, gRPC hataları)
// sonraki aşamalarda bu listeye girer.
var cliPackages = []string{
	".",
	"../../internal/client",
	"../../internal/bootstrap",
	"../../internal/anchor",
	"../../internal/domaincheck",
	"../../internal/connproto",
}

// turkishLetters, ASCII dışı Türkçe harfler. Türkçe bir metnin hepsini
// yakalamaz ("kapsam", "uygulama"), ama geri dönüşün çoğu bunlardan biriyle
// gelir; geri kalanı yazarken ve incelemede okunuyor.
const turkishLetters = "çğıöşüÇĞİÖŞÜ"

// TestCLIMessagesAreEnglish: CLI'ın metni İngilizce (K-141). Yorumlar ve
// testlerin tanı mesajları Türkçe kalabilir; burada yalnız üretim kodundaki
// dize sabitleri sayılıyor.
func TestCLIMessagesAreEnglish(t *testing.T) {
	fset := token.NewFileSet()
	checked := 0
	for _, dir := range cliPackages {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("%s okunamadı: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			path := filepath.Join(dir, name)
			f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("%s ayrıştırılamadı: %v", path, err)
			}
			checked++
			ast.Inspect(f, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				if strings.ContainsAny(lit.Value, turkishLetters) {
					t.Errorf("%s: Türkçe metin: %.80s", fset.Position(lit.Pos()), lit.Value)
				}
				return true
			})
		}
	}
	// Kontrol: liste boş ya da yollar kaymışsa test hiçbir şey ölçmeden
	// yeşil geçerdi.
	if checked < 20 {
		t.Fatalf("yalnız %d dosya denetlendi — paket yolları kaymış olabilir", checked)
	}
}

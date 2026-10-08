package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
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

// serverPackages, sunucu tarafı paketler (K-141, 3. aşama). Buradaki hata
// metni gRPC ile olduğu gibi CLI'a dönüyor (appError → err.Error()).
var serverPackages = []string{
	"../../cmd/kadrand",
	"../../cmd/kadran-connect",
	"../../cmd/kadran-exec",
	"../../cmd/kadran-vault",
	"../../internal/alarm",
	"../../internal/api",
	"../../internal/audit",
	"../../internal/deploy",
	"../../internal/dockerdrv",
	"../../internal/exec",
	"../../internal/execclient",
	"../../internal/grpcserve",
	"../../internal/health",
	"../../internal/liveness",
	"../../internal/logutil",
	"../../internal/pbconv",
	"../../internal/peercred",
	"../../internal/proxydrv",
	"../../internal/sdnotify",
	"../../internal/sockets",
	"../../internal/sshenv",
	"../../internal/store",
	"../../internal/vault",
	"../../internal/vault/vaultkey",
	"../../internal/vault/vaulttest",
}

// journalOnlyLiterals, journal'a ya da alarma giden ama slog/Detail/
// sdnotify kalıbına uymayan dizeler: 5. aşamada (journal, alarm, Telegram;
// K-108/K-114 sözleşmesi) karara bağlanacak. Dosya + dize sabiti.
var journalOnlyLiterals = map[string]bool{
	// döngü adı: watchdog günlüğünde ve e2e-watchdog beklentisinde
	`cmd/kadrand/main.go "gözetmen"`: true,
	// açılış uzlaştırmasının sonucu: alarm ayrıntısına yazılıyor
	`cmd/kadrand/main.go "ters vekil uzlaştırılamadı: "`: true,
	// "executor hazır" günlük satırının alan değeri
	`cmd/kadran-exec/main.go "YOK (kısıt uygulanmıyor)"`: true,
}

// isJournalOnly, dize sabitinin yalnız journal'a/alarma gittiğini söyler:
// bir slog çağrısının argümanı, sdnotify.Status'un argümanı ya da bir
// store.Alarm'ın Detail alanı.
func isJournalOnly(stack []ast.Node) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		switch n := stack[i].(type) {
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok {
				continue
			}
			pkg, ok := sel.X.(*ast.Ident)
			if ok && (pkg.Name == "slog" || pkg.Name == "sdnotify" && sel.Sel.Name == "Status") {
				return true
			}
		case *ast.KeyValueExpr:
			key, ok := n.Key.(*ast.Ident)
			if !ok || key.Name != "Detail" || i == 0 {
				continue
			}
			if lit, ok := stack[i-1].(*ast.CompositeLit); ok && isStoreAlarm(lit.Type) {
				return true
			}
		}
	}
	return false
}

func isStoreAlarm(t ast.Expr) bool {
	sel, ok := t.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "store" && sel.Sel.Name == "Alarm"
}

// TestServerErrorsAreEnglish: sunucunun CLI'a dönen metni (hata değerleri,
// yanıt ayrıntıları, ikililerin -h metni) İngilizce (K-141, 3. aşama).
// journal ve alarm metni 5. aşamada; isJournalOnly onları ayırıyor.
func TestServerErrorsAreEnglish(t *testing.T) {
	fset := token.NewFileSet()
	files, literals := 0, 0
	for _, dir := range serverPackages {
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
			files++
			rel := filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(path), "../../"))
			var stack []ast.Node
			ast.Inspect(f, func(n ast.Node) bool {
				if n == nil {
					stack = stack[:len(stack)-1]
					return true
				}
				stack = append(stack, n)
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				literals++
				if !strings.ContainsAny(lit.Value, turkishLetters) || isJournalOnly(stack) ||
					journalOnlyLiterals[rel+" "+lit.Value] {
					return true
				}
				t.Errorf("%s: Türkçe metin: %.80s", fset.Position(lit.Pos()), lit.Value)
				return true
			})
		}
	}
	// Kontrol: yollar kaymışsa ya da ayrıştırma boş dönerse test hiçbir şey
	// ölçmeden yeşil geçerdi.
	if files < 80 || literals < 2000 {
		t.Fatalf("yalnız %d dosya / %d dize denetlendi — paket yolları kaymış olabilir", files, literals)
	}
}

// serverScripts, sunucuda koşup operatöre metin basan betikler (K-141,
// 2. aşama). Alarm göndericisi (deploy/notify) 5. aşamada.
var serverScripts = []string{
	"../../internal/bootstrap/install.sh",
	"../../internal/bootstrap/goc.sh",
	"../../internal/bootstrap/geri.sh",
	"../../internal/bootstrap/kasa-coz.sh",
	"../../deploy/offsite/kadran-offsite.sh",
	"../../deploy/offsite/kadran-volume-backup.sh",
}

// trailingComment, satır sonu yorumu: boşluk, #, boşluk. `${#dizi[@]}`
// gibi kabuk sözdizimi önünde boşluk taşımadığı için eşleşmez.
var trailingComment = regexp.MustCompile(`\s#\s.*$`)

// TestServerScriptsAreEnglish: betiklerin kod satırları (yorum olmayan)
// Türkçe harf taşımıyor. Yorum satırları Türkçe kalabilir.
func TestServerScriptsAreEnglish(t *testing.T) {
	lines := 0
	for _, path := range serverScripts {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s okunamadı: %v", path, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}
			lines++
			code := trailingComment.ReplaceAllString(line, "")
			if strings.ContainsAny(code, turkishLetters) {
				t.Errorf("%s:%d: Türkçe metin: %.80s", path, i+1, strings.TrimSpace(code))
			}
		}
	}
	// Kontrol: yollar kaymışsa test hiçbir şey ölçmeden yeşil geçerdi.
	if lines < 500 {
		t.Fatalf("yalnız %d kod satırı denetlendi — betik yolları kaymış olabilir", lines)
	}
}

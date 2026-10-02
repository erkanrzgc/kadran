package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestLicenseFiles: modül kökündeki lisans ve NOTICE dosyaları bulunur;
// kaynak dosyası ya da alt dizindeki bir LICENSE bulunmaz (alt dizin başka
// bir paketin, modülün değil).
func TestLicenseFiles(t *testing.T) {
	dir := t.TempDir()
	writeFiles(t, dir, map[string]string{
		"LICENSE":            "a",
		"NOTICE":             "b",
		"COPYING.md":         "c",
		"LICENSE-APACHE":     "d",
		"licence.txt":        "e",
		"license.go":         "package x", // kaynak dosyası
		"licenses/LICENSE":   "f",         // alt dizin
		"LICENSE_test.go":    "package x",
		"README.md":          "g",
		"vendor/x/LICENSE":   "h",
		"UNLICENSE":          "i",
		"NOTICE.txt":         "j",
		"PATENTS":            "k",
		"third_party/NOTICE": "l",
	})

	got, err := licenseFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"COPYING.md", "LICENSE", "LICENSE-APACHE", "NOTICE", "NOTICE.txt", "PATENTS", "UNLICENSE", "licence.txt"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bulunan: %q\nbeklenen: %q", got, want)
	}
}

func TestParseGoList(t *testing.T) {
	out := strings.Join([]string{
		`{"ImportPath":"fmt","Standard":true}`,
		`{"ImportPath":"github.com/erkanrzgc/kadran/cmd/kadran","Module":{"Path":"github.com/erkanrzgc/kadran","Main":true,"Dir":"/repo"}}`,
		`{"ImportPath":"google.golang.org/grpc","Module":{"Path":"google.golang.org/grpc","Version":"v1.83.2","Dir":"/mod/grpc"}}`,
		`{"ImportPath":"google.golang.org/grpc/codes","Module":{"Path":"google.golang.org/grpc","Version":"v1.83.2","Dir":"/mod/grpc"}}`,
		`{"ImportPath":"example.com/eski","Module":{"Path":"example.com/eski","Version":"v1.0.0","Dir":"/mod/eski","Replace":{"Path":"example.com/yeni","Version":"v2.0.0","Dir":"/mod/yeni"}}}`,
	}, "\n")

	mods, std, err := parseGoList([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !std {
		t.Error("standart kütüphane kullanımı görülmedi")
	}
	want := []module{
		{Path: "example.com/yeni", Version: "v2.0.0", Dir: "/mod/yeni"},
		{Path: "google.golang.org/grpc", Version: "v1.83.2", Dir: "/mod/grpc"},
	}
	if !reflect.DeepEqual(mods, want) {
		t.Fatalf("modüller: %+v\nbeklenen: %+v", mods, want)
	}
}

// TestParseGoListRejectsAModuleWithoutDir: indirilmemiş modülün lisansı
// okunamaz; sessizce atlamak eksik bir dosya üretirdi.
func TestParseGoListRejectsAModuleWithoutDir(t *testing.T) {
	_, _, err := parseGoList([]byte(`{"ImportPath":"x/y","Module":{"Path":"x/y","Version":"v1.0.0"}}`))
	if err == nil {
		t.Fatal("dizinsiz modül kabul edildi")
	}
}

func TestCollectFailsClosedOnAMissingLicense(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"lisansli/LICENSE":  "MIT",
		"lisanssiz/main.go": "package x",
		"goroot/LICENSE":    "BSD",
	})
	usage := map[module][]string{
		{Path: "a/lisansli", Version: "v1", Dir: filepath.Join(root, "lisansli")}:   {"kadran"},
		{Path: "b/lisanssiz", Version: "v2", Dir: filepath.Join(root, "lisanssiz")}: {"kadrand"},
	}
	_, err := collect(usage, filepath.Join(root, "goroot"), "go1.25.13")
	var missing *missingLicenseError
	if !errors.As(err, &missing) {
		t.Fatalf("hata = %v, missingLicenseError bekleniyordu", err)
	}
	if !strings.Contains(err.Error(), "b/lisanssiz v2") || strings.Contains(err.Error(), "a/lisansli") {
		t.Fatalf("hata eksik modülü doğru söylemiyor: %v", err)
	}
}

func TestRenderIsDeterministicAndComplete(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"z/LICENSE":      "Z lisansı",
		"z/NOTICE":       "Z notu",
		"a/COPYING":      "A lisansı",
		"goroot/LICENSE": "Go lisansı",
	})
	usage := map[module][]string{
		{Path: "z/mod", Version: "v1.0.0", Dir: filepath.Join(root, "z")}: {"kadrand", "kadran"},
		{Path: "a/mod", Version: "v0.1.0", Dir: filepath.Join(root, "a")}: {"kadran-caddy"},
	}

	var first []byte
	for i := 0; i < 3; i++ {
		entries, err := collect(usage, filepath.Join(root, "goroot"), "go1.25.13")
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		if err := render(&buf, entries); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = buf.Bytes()
			continue
		}
		if !bytes.Equal(first, buf.Bytes()) {
			t.Fatal("çıktı koşudan koşuya değişiyor")
		}
	}

	s := string(first)
	for _, want := range []string{"Go standard library go1.25.13", "Go lisansı", "a/mod v0.1.0", "A lisansı",
		"z/mod v1.0.0", "Z lisansı", "Z notu", "Used by: kadran, kadrand", "Used by: kadran-caddy"} {
		if !strings.Contains(s, want) {
			t.Errorf("çıktıda %q yok", want)
		}
	}
	// Standart kütüphane önce, sonra modüller yol sırasıyla.
	std, a, z := strings.Index(s, "Go standard library"), strings.Index(s, "a/mod"), strings.Index(s, "z/mod")
	if std > a || a > z {
		t.Errorf("sıra yanlış:\n%s", s)
	}
}

// TestSameModuleTwoVersionsHasAFixedOrder: kök modül ile build/caddy aynı
// modülün FARKLI sürümlerini kullanıyor (ölçüldü: golang.org/x/net v0.58.0
// ve v0.55.0). Yalnız yola göre sıralama ikisinin sırasını haritanın
// dolaşım sırasına bırakırdı; çıktı koşudan koşuya değişirdi.
func TestSameModuleTwoVersionsHasAFixedOrder(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"a/LICENSE": "a", "b/LICENSE": "b", "c/LICENSE": "c", "goroot/LICENSE": "go",
	})
	usage := map[module][]string{
		{Path: "golang.org/x/net", Version: "v0.58.0", Dir: filepath.Join(root, "a")}: {"kadran"},
		{Path: "golang.org/x/net", Version: "v0.55.0", Dir: filepath.Join(root, "b")}: {"kadran-caddy"},
		{Path: "golang.org/x/net", Version: "v0.9.0", Dir: filepath.Join(root, "c")}:  {"kadrand"},
	}
	for i := 0; i < 50; i++ {
		entries, err := collect(usage, filepath.Join(root, "goroot"), "go1")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, e := range entries[1:] {
			got = append(got, e.Title)
		}
		want := []string{"golang.org/x/net v0.55.0", "golang.org/x/net v0.58.0", "golang.org/x/net v0.9.0"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("koşu %d: sıra %q, beklenen %q", i, got, want)
		}
	}

	out := strings.Join([]string{
		`{"ImportPath":"x/a","Module":{"Path":"x","Version":"v2.0.0","Dir":"/b"}}`,
		`{"ImportPath":"x/b","Module":{"Path":"x","Version":"v1.0.0","Dir":"/a"}}`,
	}, "\n")
	for i := 0; i < 50; i++ {
		mods, _, err := parseGoList([]byte(out))
		if err != nil {
			t.Fatal(err)
		}
		if mods[0].Version != "v1.0.0" || mods[1].Version != "v2.0.0" {
			t.Fatalf("koşu %d: parseGoList sırası %+v", i, mods)
		}
	}
}

// TestShippedTargetsMatchTheBuildScripts: araç, YAYINLANAN ikilileri
// tarıyor. Derleme betiğine bir ikili eklenip buraya eklenmezse onun
// bağımlılıklarının lisansları dosyaya girmezdi.
func TestShippedTargetsMatchTheBuildScripts(t *testing.T) {
	release, err := os.ReadFile(filepath.Join("..", "..", "scripts", "build-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(release), "SERVER_BINARIES=(kadrand kadran-exec kadran-connect)") {
		t.Fatal("build-release.sh'in sunucu ikili listesi değişti — targets'ı güncelleyin")
	}
	names := map[string]bool{}
	for _, tg := range targets {
		names[tg.Name] = true
	}
	for _, n := range []string{"kadran", "kadrand", "kadran-exec", "kadran-connect", "kadran-caddy"} {
		if !names[n] {
			t.Errorf("%s taranmıyor", n)
		}
	}
}

// TestReleasePlatformsMatch: paketleme betiğinin derlediği CLI platformları
// burada taranan platformlarla aynı olmalı. Betiğe bir platform eklenip
// buraya eklenmezse yalnız o platformda derlenen bağımlılıkların (ör.
// Windows'a özgü) lisansı dosyaya girmezdi.
func TestReleasePlatformsMatch(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "package-release.sh"))
	if err != nil {
		t.Fatal(err)
	}
	const key = `CLIENT_PLATFORMS="`
	s := string(b)
	i := strings.Index(s, key)
	if i < 0 {
		t.Fatal("package-release.sh'te CLIENT_PLATFORMS yok")
	}
	rest := s[i+len(key):]
	got := strings.Fields(rest[:strings.IndexByte(rest, '"')])
	if !reflect.DeepEqual(got, clientPlatforms) {
		t.Fatalf("paketleme %q, araç %q", got, clientPlatforms)
	}
}

// TestRealModulesHaveLicenses: gerçek bağımlılık ağacı. Yeni bir bağımlılık
// lisans dosyası taşımıyorsa burada (ve CI'da) kırmızı olur.
func TestRealModulesHaveLicenses(t *testing.T) {
	usage, std, err := scan(context.Background(), filepath.Join("..", ".."), targets)
	if err != nil {
		t.Fatal(err)
	}
	if !std {
		t.Fatal("standart kütüphane görülmedi — tarama bir şey ölçmüyor")
	}
	paths := map[string]bool{}
	for m := range usage {
		paths[m.Path] = true
	}
	// Kontrol grubu: bilinen bağımlılıklar listede olmalı.
	for _, want := range []string{"google.golang.org/grpc", "modernc.org/sqlite", "github.com/caddyserver/caddy/v2"} {
		if !paths[want] {
			t.Errorf("%s taramada yok", want)
		}
	}
	goroot, version, err := goEnv(context.Background(), filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := collect(usage, goroot, version); err != nil {
		t.Fatal(err)
	}
}

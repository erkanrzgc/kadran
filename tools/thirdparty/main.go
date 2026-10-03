// thirdparty, yayınlanan ikililere derlenen her modülün lisans ve NOTICE
// dosyalarını tek bir THIRD_PARTY_LICENSES.txt'de toplar (K-132).
//
// # Neden var
//
// Yayın paketleri yalnızca ikilileri taşıyordu (ölçüldü: v0.2.0'ın sunucu
// tar'ında dört ikili, tek lisans dosyası yok). İkililer Caddy ve gRPC
// (Apache-2.0), SQLite ve protobuf (BSD) gömüyor; o lisanslar ikiliyle
// birlikte metinlerinin verilmesini şart koşuyor. Bu, Kadran'ın kendi
// lisansından bağımsız bir yükümlülük.
//
// # Nasıl
//
// Her yayınlanan ikili, yayınlandığı her platform için `go list -deps`
// ile taranıyor (CGO_ENABLED=0, derleme betikleriyle aynı). Bulunan her
// modülün KÖK dizinindeki LICENSE/COPYING/NOTICE/PATENTS dosyaları
// alınıyor; Go standart kütüphanesinin lisansı da (her ikiliye derleniyor).
// Lisans dosyası bulunamayan modül HATA: eksik bir dosyayla yayın yapmak
// yerine dur.
//
// Kullanım (depo kökünden):
//
//	go run ./tools/thirdparty -o THIRD_PARTY_LICENSES.txt
//	go run ./tools/thirdparty -check
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// target, yayınlanan bir ikili.
type target struct {
	Name      string
	Dir       string // modül kökü, depo köküne göre
	Pkg       string
	Platforms []string
}

var (
	// clientPlatforms, `kadran` CLI'ının yayınlandığı platformlar.
	clientPlatforms = []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"}
	// serverPlatforms, sunucu ikililerinin platformları (build-release.sh).
	serverPlatforms = []string{"linux/amd64", "linux/arm64"}

	targets = []target{
		{"kadran", ".", "./cmd/kadran", clientPlatforms},
		{"kadrand", ".", "./cmd/kadrand", serverPlatforms},
		{"kadran-exec", ".", "./cmd/kadran-exec", serverPlatforms},
		{"kadran-connect", ".", "./cmd/kadran-connect", serverPlatforms},
		// Ayrı Go modülü (build/caddy/go.mod); build-caddy.sh ile aynı.
		{"kadran-caddy", "build/caddy", ".", serverPlatforms},
	}
)

// module, bir ikiliye derlenen üçüncü taraf modül (replace uygulanmış).
type module struct {
	Path, Version, Dir string
}

// lessModule, yol ve SONRA sürüm: aynı modülün iki sürümü (kök modül ile
// build/caddy farklı x/net kullanıyor) sabit sırada çıkmalı. Sürümler sözlük
// sırasında; amaç anlamsal sıra değil, deterministik çıktı.
func lessModule(a, b module) bool {
	if a.Path != b.Path {
		return a.Path < b.Path
	}
	return a.Version < b.Version
}

func main() {
	out := flag.String("o", "-", "çıktı dosyası (- = stdout)")
	check := flag.Bool("check", false, "yalnızca denetle: her modülün lisansı bulunuyor mu")
	root := flag.String("root", ".", "depo kökü")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, *root, *out, *check)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "thirdparty:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, root, out string, check bool) error {
	usage, std, err := scan(ctx, root, targets)
	if err != nil {
		return err
	}
	if !std {
		return errors.New("standart kütüphane hiçbir ikilide görülmedi — tarama bozuk")
	}
	goroot, version, err := goEnv(ctx, root)
	if err != nil {
		return err
	}
	entries, err := collect(usage, goroot, version)
	if err != nil {
		return err
	}
	if check {
		fmt.Fprintf(os.Stderr, "thirdparty: %d modül + standart kütüphane, hepsinin lisansı var\n", len(usage))
		return nil
	}

	var buf bytes.Buffer
	if err := render(&buf, entries); err != nil {
		return err
	}
	if out == "-" {
		_, err := os.Stdout.Write(buf.Bytes())
		return err
	}
	return os.WriteFile(out, buf.Bytes(), 0o644) //nolint:gosec // yayın dosyası, herkes okur
}

// scan, her hedefi her platformunda tarar; modül → onu kullanan ikililer.
func scan(ctx context.Context, root string, ts []target) (map[module][]string, bool, error) {
	usage := map[module][]string{}
	std := false
	for _, t := range ts {
		for _, p := range t.Platforms {
			goos, goarch, ok := strings.Cut(p, "/")
			if !ok {
				return nil, false, fmt.Errorf("geçersiz platform %q", p)
			}
			out, err := goList(ctx, filepath.Join(root, t.Dir), t.Pkg, goos, goarch)
			if err != nil {
				return nil, false, fmt.Errorf("%s (%s): %w", t.Name, p, err)
			}
			mods, s, err := parseGoList(out)
			if err != nil {
				return nil, false, fmt.Errorf("%s (%s): %w", t.Name, p, err)
			}
			std = std || s
			for _, m := range mods {
				if !slices.Contains(usage[m], t.Name) {
					usage[m] = append(usage[m], t.Name)
				}
			}
		}
	}
	return usage, std, nil
}

func goList(ctx context.Context, dir, pkg, goos, goarch string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-deps", "-json=ImportPath,Standard,Module", pkg)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOFLAGS=-mod=readonly")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

type listedPackage struct {
	ImportPath string
	Standard   bool
	Module     *struct {
		Path, Version, Dir string
		Main               bool
		Replace            *struct{ Path, Version, Dir string }
	}
}

// parseGoList, `go list -deps -json` çıktısından üçüncü taraf modülleri
// (sıralı, tekil) ve standart kütüphanenin kullanılıp kullanılmadığını
// çıkarır. Ana modül (Kadran'ın kendisi) dışarıda.
func parseGoList(out []byte) ([]module, bool, error) {
	seen := map[module]bool{}
	std := false
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p listedPackage
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, false, fmt.Errorf("go list çıktısı çözülemedi: %w", err)
		}
		switch {
		case p.Standard:
			std = true
		case p.Module == nil || p.Module.Main:
			// Ana modülün paketleri: Kadran'ın kendi lisansı.
		default:
			m := module{Path: p.Module.Path, Version: p.Module.Version, Dir: p.Module.Dir}
			if r := p.Module.Replace; r != nil {
				m = module{Path: r.Path, Version: r.Version, Dir: r.Dir}
			}
			if m.Dir == "" {
				return nil, false, fmt.Errorf("%s %s indirilmemiş (Dir boş) — lisansı okunamaz", m.Path, m.Version)
			}
			seen[m] = true
		}
	}
	mods := make([]module, 0, len(seen))
	for m := range seen {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return lessModule(mods[i], mods[j]) })
	return mods, std, nil
}

// licenseName, modül kökünde aranan dosya adları. PATENTS: golang.org/x
// modüllerindeki ek patent izni. Kaynak dosyaları (`license.go`) dışarıda.
var licenseName = regexp.MustCompile(`(?i)^(licen[cs]e|copying|notice|unlicense|patents)([-._][^/]*)?$`)

// licenseFiles, dizinin KÖKÜNDEKİ lisans dosyalarını sıralı döndürür. Alt
// dizinler bakılmıyor: oradaki bir LICENSE modülün değil, vendor'lanmış
// başka bir kodun.
func licenseFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if !e.Type().IsRegular() || strings.HasSuffix(strings.ToLower(n), ".go") || !licenseName.MatchString(n) {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func goEnv(ctx context.Context, root string) (goroot, version string, err error) {
	cmd := exec.CommandContext(ctx, "go", "env", "GOROOT", "GOVERSION")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("go env: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != 2 {
		return "", "", fmt.Errorf("go env beklenmedik çıktı: %q", out)
	}
	return strings.TrimSpace(lines[0]), strings.TrimSpace(lines[1]), nil
}

type licenseFile struct{ Name, Text string }

type entry struct {
	Title  string
	UsedBy []string
	Files  []licenseFile
}

// missingLicenseError, lisans dosyası bulunamayan modülleri listeler.
type missingLicenseError struct{ Modules []string }

func (e *missingLicenseError) Error() string {
	return "lisans dosyası bulunamayan modüller (yayın DURDURULDU): " + strings.Join(e.Modules, ", ")
}

// collect, standart kütüphaneyi ve modülleri lisans metinleriyle toplar.
func collect(usage map[module][]string, goroot, goVersion string) ([]entry, error) {
	stdFiles, err := readLicenses(goroot)
	if err != nil {
		return nil, fmt.Errorf("standart kütüphanenin (Go) lisansı okunamadı (%s): %w", goroot, err)
	}
	if len(stdFiles) == 0 {
		return nil, fmt.Errorf("standart kütüphanenin (Go) lisansı bulunamadı (%s)", goroot)
	}
	entries := []entry{{Title: "Go standard library " + goVersion, UsedBy: []string{"every binary"}, Files: stdFiles}}

	mods := make([]module, 0, len(usage))
	for m := range usage {
		mods = append(mods, m)
	}
	sort.Slice(mods, func(i, j int) bool { return lessModule(mods[i], mods[j]) })

	var missing []string
	for _, m := range mods {
		files, err := readLicenses(m.Dir)
		if err != nil {
			return nil, fmt.Errorf("%s %s: %w", m.Path, m.Version, err)
		}
		if len(files) == 0 {
			missing = append(missing, m.Path+" "+m.Version)
			continue
		}
		users := slices.Clone(usage[m])
		sort.Strings(users)
		entries = append(entries, entry{Title: m.Path + " " + m.Version, UsedBy: users, Files: files})
	}
	if len(missing) > 0 {
		return nil, &missingLicenseError{Modules: missing}
	}
	return entries, nil
}

func readLicenses(dir string) ([]licenseFile, error) {
	names, err := licenseFiles(dir)
	if err != nil {
		return nil, err
	}
	files := make([]licenseFile, 0, len(names))
	for _, n := range names {
		b, err := os.ReadFile(filepath.Join(dir, n)) //nolint:gosec // modül önbelleğindeki dosya
		if err != nil {
			return nil, err
		}
		text := strings.ReplaceAll(string(b), "\r\n", "\n")
		files = append(files, licenseFile{Name: n, Text: strings.TrimRight(text, "\n")})
	}
	return files, nil
}

const header = `THIRD-PARTY SOFTWARE LICENSES

The Kadran binaries (kadran, kadrand, kadran-exec, kadran-connect, kadran-caddy)
are built from Kadran's own source, licensed under the Apache License 2.0 (see
LICENSE and NOTICE), and from the third-party software listed below. Each entry
reproduces the license, notice and patent files shipped with that software.

Generated by tools/thirdparty from ` + "`go list -deps`" + ` for every shipped binary
and platform. Do not edit by hand.
`

func render(w io.Writer, entries []entry) error {
	var b strings.Builder
	b.WriteString(header)
	for _, e := range entries {
		b.WriteString("\n" + strings.Repeat("=", 80) + "\n")
		b.WriteString(e.Title + "\n")
		b.WriteString("Used by: " + strings.Join(e.UsedBy, ", ") + "\n")
		for _, f := range e.Files {
			b.WriteString("\n--- " + f.Name + " ---\n\n")
			b.WriteString(f.Text + "\n")
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

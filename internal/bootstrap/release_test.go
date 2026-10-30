package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// ── Sürümden kurulum: indir, gömülü özetle doğrula, aç (K-159) ────────

// paketTar, verilen dosyalarla (ad → içerik) gzip'li bir tar üretir. Ek
// başlıklar (bağ, aygıt…) içeriksiz, dosyalardan sonra eklenir.
func paketTar(t *testing.T, files map[string]string, extra ...*tar.Header) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	slices.Sort(names)
	for _, n := range names {
		c := files[n]
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(c)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(c)); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range extra {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ozet(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// paketSunucusu, gövdeyi YALNIZ doğru yolda sunar (URL'nin kurulumu da
// sınansın) ve gelen istekleri sayar.
func paketSunucusu(t *testing.T, body []byte, status int) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var n atomic.Int32
	const yol = "/v9.9.9/kadran-server-v9.9.9-linux-amd64.tar.gz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		if r.URL.Path != yol {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &n
}

// geciciTaban, FetchRelease'in geçici dizinlerini test dizinine yönlendirir;
// geride kalan sayılabilsin.
func geciciTaban(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv("TMPDIR", d)
	t.Setenv("TMP", d)
	t.Setenv("TEMP", d)
	return d
}

func kalanlar(t *testing.T, d string) []string {
	t.Helper()
	entries, err := os.ReadDir(d)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "kadran-release-") {
			out = append(out, e.Name())
		}
	}
	return out
}

func gecerliPaket(t *testing.T) []byte {
	return paketTar(t, map[string]string{
		"bin/linux-amd64/kadrand":        "daemon",
		"deploy/systemd/kadrand.service": "unit",
		"LICENSE":                        "license",
	})
}

func TestBundleName(t *testing.T) {
	if got := BundleName("v0.9.3", "arm64"); got != "kadran-server-v0.9.3-linux-arm64.tar.gz" {
		t.Fatalf("BundleName = %q", got)
	}
}

func TestFetchReleaseVerifiesAndUnpacks(t *testing.T) {
	geciciTaban(t)
	body := gecerliPaket(t)
	srv, _ := paketSunucusu(t, body, http.StatusOK)
	var out bytes.Buffer

	dir, cleanup, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": ozet(body)}}, "amd64", &out)
	if err != nil {
		t.Fatal(err)
	}
	for ad, icerik := range map[string]string{
		"bin/linux-amd64/kadrand":        "daemon",
		"deploy/systemd/kadrand.service": "unit",
		"LICENSE":                        "license",
	} {
		b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(ad)))
		if err != nil || string(b) != icerik {
			t.Errorf("%s: %q, %v", ad, b, err)
		}
	}
	if !strings.Contains(out.String(), "Verified sha256 "+ozet(body)[:12]) {
		t.Errorf("çıktıda doğrulama satırı yok:\n%s", out.String())
	}
	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup sonrası dizin duruyor: %v", err)
	}
}

func TestFetchReleaseRejectsTamperedBundle(t *testing.T) {
	taban := geciciTaban(t)
	beklenen := gecerliPaket(t)
	sahte := paketTar(t, map[string]string{"bin/linux-amd64/kadrand": "evil"})
	srv, _ := paketSunucusu(t, sahte, http.StatusOK)

	dir, _, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": ozet(beklenen)}}, "amd64", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "does not match the hash built into this kadran") {
		t.Fatalf("hata = %v", err)
	}
	if dir != "" {
		t.Errorf("dir = %q, boş beklendi", dir)
	}
	if k := kalanlar(t, taban); len(k) != 0 {
		t.Errorf("geçici dizin kaldı: %v", k)
	}
}

func TestFetchReleaseRefusesMissingHash(t *testing.T) {
	geciciTaban(t)
	srv, n := paketSunucusu(t, gecerliPaket(t), http.StatusOK)

	_, _, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": strings.Repeat("a", 64)}}, "arm64", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "built without a server bundle hash for linux/arm64") ||
		!strings.Contains(err.Error(), "-binaries") {
		t.Fatalf("hata = %v", err)
	}
	if got := n.Load(); got != 0 {
		t.Errorf("özet yokken %d istek gitti", got)
	}
}

func TestFetchReleaseNotFound(t *testing.T) {
	geciciTaban(t)
	srv, _ := paketSunucusu(t, []byte("yok"), http.StatusNotFound)

	_, _, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": strings.Repeat("a", 64)}}, "amd64", &bytes.Buffer{})
	url := srv.URL + "/v9.9.9/kadran-server-v9.9.9-linux-amd64.tar.gz"
	if err == nil || !strings.Contains(err.Error(), url) || !strings.Contains(err.Error(), "404") ||
		!strings.Contains(err.Error(), "-binaries") {
		t.Fatalf("hata = %v", err)
	}
}

func TestFetchReleaseCleansUpOnShortBody(t *testing.T) {
	taban := geciciTaban(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "100000")
		_, _ = w.Write([]byte("yarim"))
	}))
	t.Cleanup(srv.Close)

	_, _, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": strings.Repeat("a", 64)}}, "amd64", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("hata = %v", err)
	}
	if k := kalanlar(t, taban); len(k) != 0 {
		t.Errorf("geçici dizin kaldı: %v", k)
	}
}

func TestFetchReleaseSizeCap(t *testing.T) {
	geciciTaban(t)
	defer func(v int64) { maxBundleBytes = v }(maxBundleBytes)
	maxBundleBytes = 1024
	body := bytes.Repeat([]byte("x"), 4096)
	srv, _ := paketSunucusu(t, body, http.StatusOK)

	_, _, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": ozet(body)}}, "amd64", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("hata = %v", err)
	}
}

// TestUnpackRejectsUnsafeEntries: özet tutsa bile paketin dışına yazan,
// bağ ya da aygıt taşıyan, izinli köklerin dışında kalan girdi reddedilir.
// Özet tutması bir güvence, ama tek güvence olmasın.
func TestUnpackRejectsUnsafeEntries(t *testing.T) {
	ok := map[string]string{"bin/linux-amd64/kadrand": "daemon"}
	for _, tc := range []struct {
		ad    string
		files map[string]string
		extra []*tar.Header
	}{
		{"üst dizin", map[string]string{"../evil": "x"}, nil},
		{"mutlak yol", map[string]string{"/etc/passwd": "x"}, nil},
		{"yol içinde ..", map[string]string{"bin/../../evil": "x"}, nil},
		{"sembolik bağ", ok, []*tar.Header{{Name: "bin/l", Linkname: "/etc", Typeflag: tar.TypeSymlink}}},
		{"sabit bağ", ok, []*tar.Header{{Name: "bin/h", Linkname: "bin/linux-amd64/kadrand", Typeflag: tar.TypeLink}}},
		{"karakter aygıtı", ok, []*tar.Header{{Name: "bin/c", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3}}},
		{"izinli kök dışı", map[string]string{"etc/x": "x"}, nil},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			taban := geciciTaban(t)
			body := paketTar(t, tc.files, tc.extra...)
			srv, _ := paketSunucusu(t, body, http.StatusOK)

			_, _, err := FetchRelease(context.Background(),
				Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": ozet(body)}}, "amd64", &bytes.Buffer{})
			if err == nil {
				t.Fatal("kabul edildi")
			}
			if _, err := os.Stat(filepath.Join(taban, "evil")); !os.IsNotExist(err) {
				t.Error("paketin dışına yazıldı")
			}
			if k := kalanlar(t, taban); len(k) != 0 {
				t.Errorf("geçici dizin kaldı: %v", k)
			}
		})
	}
}

func TestUnpackSizeCap(t *testing.T) {
	geciciTaban(t)
	defer func(v int64) { maxUnpackedBytes = v }(maxUnpackedBytes)
	maxUnpackedBytes = 10
	body := paketTar(t, map[string]string{"bin/linux-amd64/kadrand": "on bir bayt"})
	srv, _ := paketSunucusu(t, body, http.StatusOK)

	_, _, err := FetchRelease(context.Background(),
		Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": ozet(body)}}, "amd64", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "unpacked size") {
		t.Fatalf("hata = %v", err)
	}
}

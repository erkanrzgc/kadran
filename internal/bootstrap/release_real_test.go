package bootstrap

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestRealReleaseBundle: package-release.sh'in ürettiği GERÇEK paket,
// bootstrap'ın okuduğu her dosyayı taşıyor mu (K-159). Paketin tar listesi ile
// unitFiles/migrationFiles birbirinden habersiz; eksik bir dosya o sürümün
// BÜTÜN kurulumlarını düşürürdü ve özet sonradan değişmez.
//
// Gerçek paket yalnız CI'ın paket işinde var. Orada adım, testin gerçekten
// koştuğunu sonuç satırından doğruluyor: atlanması yeşil sayılmaz.
func TestRealReleaseBundle(t *testing.T) {
	kok, ver := os.Getenv("KADRAN_TEST_RELEASE_DIR"), os.Getenv("KADRAN_TEST_RELEASE_VERSION")
	if kok == "" || ver == "" {
		t.Skip("KADRAN_TEST_RELEASE_DIR ve KADRAN_TEST_RELEASE_VERSION yok")
	}
	geciciTaban(t)
	srv := httptest.NewServer(http.StripPrefix("/"+ver, http.FileServer(http.Dir(kok))))
	t.Cleanup(srv.Close)
	key := filepath.Join(t.TempDir(), "key.pub")
	if err := os.WriteFile(key, []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			paket, err := os.ReadFile(filepath.Join(kok, BundleName(ver, arch)))
			if err != nil {
				t.Fatal(err)
			}
			dir, cleanup, err := FetchRelease(context.Background(),
				Release{Version: ver, BaseURL: srv.URL, SHA256: map[string]string{arch: ozet(paket)}}, arch, &bytes.Buffer{})
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if _, err := buildArchive(Options{BinaryDir: filepath.Join(dir, "bin"), RepoRoot: dir, ClientKeyPath: key}, arch); err != nil {
				t.Fatal(err)
			}
		})
	}
}

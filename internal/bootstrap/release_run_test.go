package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// depoPaketi, sahte deponun bin/ ve deploy/ ağaçlarından yayındaki gibi bir
// sunucu paketi üretir.
func depoPaketi(t *testing.T, repo string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, kok := range []string{"bin", "deploy"} {
		err := filepath.WalkDir(filepath.Join(repo, kok), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if err := tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0o755, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
				return err
			}
			_, err = tw.Write(b)
			return err
		})
		if err != nil {
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

// TestBootstrapSourceLine: kullanıcı hangi dosyaların kurulduğunu çıktıdan
// görür. Yerel bin/'i olan bir depodan yayın istemcisi koşturan biri, yayının
// mı yerel dosyaların mı kurulduğunu tahmin etmek zorunda kalmasın.
func TestBootstrapSourceLine(t *testing.T) {
	t.Run("yayın", func(t *testing.T) {
		sahteSSH(t, nil)
		geciciTaban(t)
		repo := newFakeRepo(t)
		body := depoPaketi(t, repo)
		srv, _ := paketSunucusu(t, body, http.StatusOK)

		opts := kurulumSecenekleri(t, repo, "root@sunucu", false)
		// Yerel yollar BOŞ bir yeri gösteriyor: kurulum yine geçiyorsa
		// dosyalar yayından gelmiştir.
		opts.BinaryDir = filepath.Join(t.TempDir(), "yok")
		opts.RepoRoot = opts.BinaryDir
		opts.Release = &Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": ozet(body)}}
		var out bytes.Buffer
		opts.Stdout = &out

		if err := Run(context.Background(), opts); err != nil {
			t.Fatalf("kurulum düştü: %v\n%s", err, out.String())
		}
		if !strings.Contains(out.String(), "==> Server files: release v9.9.9 (verified)") {
			t.Errorf("kaynak satırı yok:\n%s", out.String())
		}
	})
	t.Run("yerel", func(t *testing.T) {
		sahteSSH(t, nil)
		repo := newFakeRepo(t)
		opts := kurulumSecenekleri(t, repo, "root@sunucu", false)
		var out bytes.Buffer
		opts.Stdout = &out

		if err := Run(context.Background(), opts); err != nil {
			t.Fatalf("kurulum düştü: %v", err)
		}
		want := "==> Server files: local (" + opts.BinaryDir + ", " + opts.RepoRoot + ")"
		if !strings.Contains(out.String(), want) {
			t.Errorf("%q yok:\n%s", want, out.String())
		}
	})
}

// TestBootstrapReleaseMismatchSendsNothing: özet tutmazsa sunucuya yalnız
// salt okunur iki yoklama gider (mimari, yetki); hiçbir dosya yüklenmez,
// kurulum başlamaz.
func TestBootstrapReleaseMismatchSendsNothing(t *testing.T) {
	log := sahteSSH(t, nil)
	geciciTaban(t)
	repo := newFakeRepo(t)
	srv, _ := paketSunucusu(t, depoPaketi(t, repo), http.StatusOK)

	opts := kurulumSecenekleri(t, repo, "root@sunucu", false)
	opts.Release = &Release{Version: "v9.9.9", BaseURL: srv.URL, SHA256: map[string]string{"amd64": strings.Repeat("0", 64)}}

	err := Run(context.Background(), opts)
	if err == nil || !strings.Contains(err.Error(), "does not match the hash built into this kadran") {
		t.Fatalf("hata = %v", err)
	}
	if c := cagrilar(t, log); strings.Join(c, "|") != "uname|yetki" {
		t.Fatalf("çağrılar: %q, beklenen yalnız uname ve yetki", c)
	}
}

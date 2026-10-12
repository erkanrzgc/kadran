package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"testing"
)

// testdata/real-archive.tar.gz GERÇEK bir `git archive` çıktısıdır
// (sentetik değil). Üretim komutları:
//
//	git init . && git config core.autocrlf false
//	# Dockerfile ("FROM scratch\n") ve src/app.txt ("hello\n") eklenip
//	# sabit tarihli tek commit atıldı
//	git archive --format=tar.gz --prefix=octocat-Hello-World-7fd1a60/ HEAD
//
// GitHub'ın codeload arşivleri de aynı biçimde ilk kayıt olarak
// `pax_global_header` (typeflag 'g') taşır.
const realArchiveFixture = "testdata/real-archive.tar.gz"

// readTypeflags, arşivdeki kayıt türlerini sırayla döndürür.
func readTypeflags(t *testing.T, gz []byte) []byte {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(zr)
	var flags []byte
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return flags
		}
		if err != nil {
			t.Fatal(err)
		}
		flags = append(flags, h.Typeflag)
	}
}

func TestPrivateArchiveAcceptsRealGitArchiveWithPaxGlobalHeader(t *testing.T) {
	fixture, err := os.ReadFile(realArchiveFixture)
	if err != nil {
		t.Fatal(err)
	}
	// Fikstür gerçekten 'g' kaydı taşımıyorsa bu test hiçbir şey
	// ölçmez; o yüzden önce bunu doğruluyoruz.
	if !bytes.Contains(readTypeflags(t, fixture), []byte{tar.TypeXGlobalHeader}) {
		t.Fatal("fixture has no pax global header ('g') entry; it no longer exercises the bug")
	}

	var out bytes.Buffer
	limits := archiveLimits{1 << 20, 1 << 20, 100}
	if err := rewritePrivateArchive(context.Background(), bytes.NewReader(fixture), &out, limits); err != nil {
		t.Fatalf("real git archive rejected: %v", err)
	}

	names := map[string]bool{}
	tr := tar.NewReader(&out)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names[h.Name] = true
	}
	for _, want := range []string{"Dockerfile", "src", "src/app.txt"} {
		if !names[want] {
			t.Fatalf("rewritten context lacks %q: %v", want, names)
		}
	}
	if names["pax_global_header"] {
		t.Fatal("global header leaked into the build context")
	}
}

// 'g' kaydı dışında hiçbir tür gevşetilmez.
func TestPrivateArchiveStillRejectsLinksAndDevices(t *testing.T) {
	for name, flag := range map[string]byte{
		"symlink":  tar.TypeSymlink,
		"hardlink": tar.TypeLink,
		"char":     tar.TypeChar,
		"fifo":     tar.TypeFifo,
	} {
		t.Run(name, func(t *testing.T) {
			body := gitArchive(t,
				&tar.Header{Name: "root/", Typeflag: tar.TypeDir, Mode: 0o755},
				&tar.Header{Name: "root/x", Typeflag: flag, Linkname: "/etc/passwd", Mode: 0o644},
			)
			err := rewritePrivateArchive(context.Background(), bytes.NewReader(body), io.Discard, archiveLimits{1 << 20, 1 << 20, 100})
			if err == nil {
				t.Fatalf("%s entry accepted", name)
			}
		})
	}
}

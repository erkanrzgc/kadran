package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"testing"
)

func gitArchive(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(entry); err != nil {
			t.Fatal(err)
		}
		if entry.Typeflag == tar.TypeReg && entry.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(entry.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func goodGitArchive(t *testing.T) []byte {
	return gitArchive(t, &tar.Header{Name: "octocat-Hello-World-7fd1a60/", Typeflag: tar.TypeDir, Mode: 0o755}, &tar.Header{Name: "octocat-Hello-World-7fd1a60/Dockerfile", Typeflag: tar.TypeReg, Mode: 0o644, Size: 12})
}
func TestPrivateArchiveBoundsAndCorruption(t *testing.T) {
	archive := goodGitArchive(t)
	for _, tc := range []struct {
		name   string
		body   []byte
		limits archiveLimits
	}{
		{"compressed", archive, archiveLimits{int64(len(archive) - 1), 1 << 20, 100}},
		{"expanded", archive, archiveLimits{1 << 20, 600, 100}},
		{"entries", archive, archiveLimits{1 << 20, 1 << 20, 1}},
		{"truncated gzip", archive[:len(archive)-5], archiveLimits{1 << 20, 1 << 20, 100}},
		{"duplicate path", gitArchive(t, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}), archiveLimits{1 << 20, 1 << 20, 100}},
		{"multiple roots", gitArchive(t, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}, &tar.Header{Name: "other/file", Typeflag: tar.TypeReg}), archiveLimits{1 << 20, 1 << 20, 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := rewritePrivateArchive(context.Background(), bytes.NewReader(tc.body), io.Discard, tc.limits); err == nil {
				t.Fatal("unbounded or corrupt archive accepted")
			}
		})
	}
}

func TestPrivateArchiveRejectsUnsafeTarEntries(t *testing.T) {
	for _, entry := range []*tar.Header{
		{Name: "root/../outside", Typeflag: tar.TypeReg}, {Name: "/root/outside", Typeflag: tar.TypeReg}, {Name: "root/C:outside", Typeflag: tar.TypeReg},
		{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}, {Name: "root/link", Typeflag: tar.TypeLink, Linkname: "../../etc/passwd"}, {Name: "root/device", Typeflag: tar.TypeChar},
	} {
		t.Run(entry.Name, func(t *testing.T) {
			if err := rewritePrivateArchive(context.Background(), bytes.NewReader(gitArchive(t, entry)), io.Discard, defaultArchiveLimits); err == nil {
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

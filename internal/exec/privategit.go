package exec

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path"
	"strings"
)

type archiveLimits struct {
	compressed, expanded int64
	entries              int
}

// defaultArchiveLimits, kadran-git'ten gelen arşivin üst sınırlarıdır.
var defaultArchiveLimits = archiveLimits{128 << 20, 512 << 20, 100000}

func rewritePrivateArchive(ctx context.Context, r io.Reader, w io.Writer, limits archiveLimits) error {
	compressed := &io.LimitedReader{R: r, N: limits.compressed + 1}
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return errors.New("private Git archive is not valid gzip")
	}
	defer gz.Close()
	expanded := &io.LimitedReader{R: gz, N: limits.expanded + 1}
	tr, tw := tar.NewReader(expanded), tar.NewWriter(w)
	root, seen, count := "", map[string]bool{}, 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("private Git archive is corrupt or exceeds expanded limit")
		}
		count++
		// `git archive` ve codeload ilk kayıt olarak pax genel başlığı
		// (`pax_global_header`, typeflag 'g') yazar. Yalnız bu tür atlanır:
		// içeriği uygulanmaz, bağlama yazılmaz, kök mantığına katılmaz.
		// Symlink, hardlink, aygıt vb. aşağıda reddedilmeye devam eder.
		if h.Typeflag == tar.TypeXGlobalHeader {
			if count > limits.entries {
				return errors.New("unsafe or oversized private Git archive entry")
			}
			continue
		}
		name := strings.TrimSuffix(h.Name, "/")
		first, relative, _ := strings.Cut(name, "/")
		if count > limits.entries || path.IsAbs(name) || path.Clean(name) != name || strings.ContainsAny(name, "\\:") || first == "." || first == ".." || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || h.Size < 0 || h.Size > limits.expanded {
			return errors.New("unsafe or oversized private Git archive entry")
		}
		if root == "" {
			root = first
		}
		if first != root {
			return errors.New("private Git archive has multiple roots")
		}
		if relative == "" {
			if h.Typeflag != tar.TypeDir {
				return errors.New("private Git archive has no root directory")
			}
			continue
		}
		if seen[relative] {
			return errors.New("private Git archive has duplicate paths")
		}
		seen[relative] = true
		if err := tw.WriteHeader(&tar.Header{Name: relative, Mode: h.Mode & 0o777, Typeflag: h.Typeflag, Size: h.Size, ModTime: h.ModTime}); err != nil {
			return errors.New("could not write private build context")
		}
		if _, err := io.CopyN(tw, tr, h.Size); err != nil {
			return errors.New("private Git archive body is corrupt or context storage failed")
		}
	}
	// Validate gzip checksum/truncation and bound padding after tar's EOF.
	if _, err := io.Copy(io.Discard, expanded); err != nil || expanded.N <= 0 || compressed.N <= 0 || len(seen) == 0 {
		return errors.New("private Git archive is corrupt, empty or oversized")
	}
	if err := tw.Close(); err != nil {
		return errors.New("could not complete private build context")
	}
	return nil
}
func closePrivateContext(f *os.File) {
	if f != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
}

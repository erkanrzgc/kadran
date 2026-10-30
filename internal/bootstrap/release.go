package bootstrap

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/erkanrzgc/kadran/internal/version"
)

// ── Sürümden kurulum (K-159) ──────────────────────────────────────────
//
// İstemci kendi sürümünün sunucu paketini GitHub yayınından indirir ve
// YALNIZ ikilisine gömülü özetle (version.ServerSHA256*) eşleşirse açar.
// Aynı yayındaki SHA256SUMS bozulmayı yakalar ama sahteciliği yakalamaz:
// yayını değiştirebilen, onu da değiştirir. İstemciye güvenen, kuracağı
// sunucuya da güvenmiş olur; ek bir anahtar yok.

// DefaultReleaseBase, yayın dosyalarının indirildiği adres (sürüm ve dosya
// adı arkasına eklenir). KADRAN_RELEASE_BASE ile bir yansıya çevrilebilir;
// özet denetimi yine geçerli.
const DefaultReleaseBase = "https://github.com/erkanrzgc/kadran/releases/download"

// Release, bir istemcinin kendi sunucu paketinin nerede olduğunu ve hangi
// özete sahip olması gerektiğini söyler.
type Release struct {
	Version string            // ör. "v0.9.3"
	BaseURL string            // sonunda / olmadan
	SHA256  map[string]string // "amd64"/"arm64" → küçük harf onaltılık
}

// Sınırlar: paket bugün ~30 MiB, açılmış hâli ~75 MiB. Testler düşürür.
var (
	maxBundleBytes   int64 = 256 << 20
	maxUnpackedBytes int64 = 512 << 20
)

// BundleName, yayındaki sunucu paketinin adı (package-release.sh ile aynı).
func BundleName(ver, arch string) string {
	return "kadran-server-" + ver + "-linux-" + arch + ".tar.gz"
}

// FetchRelease, <BaseURL>/<Version>/<BundleName> dosyasını indirir, SHA256'sı
// r.SHA256[arch] değilse reddeder ve yeni bir geçici dizine açar. dir,
// bin/linux-<arch>/… ve deploy/… taşır; cleanup onu siler. Hata olursa geride
// geçici dizin kalmaz.
func FetchRelease(ctx context.Context, r Release, arch string, out io.Writer) (dir string, cleanup func(), err error) {
	want := strings.ToLower(strings.TrimSpace(r.SHA256[arch]))
	if !isSHA256Hex(want) {
		return "", nil, fmt.Errorf(
			"bootstrap: this kadran (%s) was built without a server bundle hash for linux/%s; "+
				"install from local files with -binaries <dir> -repo <dir>", r.Version, arch)
	}
	name := BundleName(r.Version, arch)
	u := strings.TrimRight(r.BaseURL, "/") + "/" + url.PathEscape(r.Version) + "/" + name

	tmp, err := os.MkdirTemp("", "kadran-release-*")
	if err != nil {
		return "", nil, fmt.Errorf("bootstrap: could not create a temporary directory: %w", err)
	}
	remove := func() { _ = os.RemoveAll(tmp) }
	defer func() {
		if err != nil {
			remove()
		}
	}()

	host := u
	if p, perr := url.Parse(u); perr == nil {
		host = p.Host
	}
	fmt.Fprintf(out, "==> Downloading %s from %s\n", name, host)
	tarball := filepath.Join(tmp, name)
	var got string
	got, err = download(ctx, u, tarball)
	if err != nil {
		return "", nil, err
	}
	// Sır karşılaştırılmıyor; sabit süreli karşılaştırma yalnız niyeti
	// okuyana belli etmek için.
	if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		err = fmt.Errorf("bootstrap: %s has sha256 %s, which does not match the hash built into this kadran (%s); "+
			"nothing was installed", u, got, want)
		return "", nil, err
	}
	fmt.Fprintf(out, "==> Verified sha256 %s… (matches the hash built into this kadran)\n", got[:12])

	root := filepath.Join(tmp, "bundle")
	if err = unpackBundle(tarball, root); err != nil {
		return "", nil, err
	}
	_ = os.Remove(tarball)
	return root, remove, nil
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

// download, u'yu dst'ye yazar ve SHA256'sını döner. İstemcinin Timeout'u
// yok: gövdeyi de keserdi; süreyi bootstrap'ın bağlamı sınırlıyor.
func download(ctx context.Context, u, dst string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("bootstrap: download %s: %w", u, err)
	}
	req.Header.Set("User-Agent", "kadran/"+version.Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("bootstrap: download %s: %w", u, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bootstrap: download %s: %s (is this release published?); "+
			"to install from local files use -binaries <dir> -repo <dir>", u, resp.Status)
	}

	f, err := os.Create(dst)
	if err != nil {
		return "", fmt.Errorf("bootstrap: %w", err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, maxBundleBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("bootstrap: download %s: %w", u, err)
	}
	if n > maxBundleBytes {
		return "", fmt.Errorf("bootstrap: %s is larger than %d bytes; refusing it", u, maxBundleBytes)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// unpackBundle, doğrulanmış paketi root'a açar. Özet tutması bir güvence ama
// tek güvence olmasın: yalnız dizin ve düz dosya, yalnız izinli kökler,
// paketin dışına çıkan ad yok, toplam boyut sınırlı.
func unpackBundle(tarball, root string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("bootstrap: reading the server bundle: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var total int64
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("bootstrap: reading the server bundle: %w", err)
		}
		rel, err := bundlePath(hdr.Name)
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}
		case tar.TypeReg:
			total += hdr.Size
			if hdr.Size < 0 || total > maxUnpackedBytes {
				return fmt.Errorf("bootstrap: the server bundle's unpacked size is over %d bytes", maxUnpackedBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}
			if err := writeBundleFile(target, tr, hdr.Size, hdr.FileInfo().Mode().Perm()); err != nil {
				return err
			}
		default:
			return fmt.Errorf("bootstrap: the server bundle has an unexpected entry %q (type %q)", hdr.Name, hdr.Typeflag)
		}
	}
}

// bundlePath, tar girdisinin adını denetler ve temizlenmiş, eğik çizgili
// göreli yolu döner.
func bundlePath(name string) (string, error) {
	bad := func() (string, error) {
		return "", fmt.Errorf("bootstrap: the server bundle has an unsafe entry %q", name)
	}
	if name == "" || strings.HasPrefix(name, "/") || strings.ContainsAny(name, `\:`) {
		return bad()
	}
	for _, seg := range strings.Split(name, "/") {
		if seg == ".." {
			return bad()
		}
	}
	clean := path.Clean(name)
	switch {
	case clean == "LICENSE", clean == "THIRD_PARTY_LICENSES.txt",
		clean == "bin", strings.HasPrefix(clean, "bin/"),
		clean == "deploy", strings.HasPrefix(clean, "deploy/"):
		return clean, nil
	}
	return bad()
}

func writeBundleFile(target string, r io.Reader, size int64, perm os.FileMode) error {
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("bootstrap: %w", err)
	}
	_, err = io.CopyN(f, r, size)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("bootstrap: unpacking %s: %w", target, err)
	}
	return nil
}

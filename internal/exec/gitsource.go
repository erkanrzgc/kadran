package exec

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// DefaultGitHost, `-allow-git-host` verilmediğinde izin verilen tek host.
const DefaultGitHost = "github.com"

const (
	maxOwnerLen      = 100
	maxRepoLen       = 100
	maxHostLen       = 253 // RFC 1035
	maxDockerfileLen = 4096
	maxBuildArgs     = 100
)

var (
	// hostPattern, şema/port/yol/kullanıcı bilgisi İÇEREMEZ. `:` ve `/`
	// karakter kümesinde olmadığı için bunlar temsil edilemez.
	hostPattern = regexp.MustCompile(
		`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

	// pathSegPattern, owner ve repo için. Eğik çizgi YOK: ek yol
	// segmenti enjekte edilemez.
	pathSegPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)

	// fullSHAPattern, TAM 40 hane. Kısa sha veya dal adı kabul edilmez.
	//
	// Bu desen, CVE-2026-33748'in sınıfını temsil edilemez kılan yerdir:
	// fragment'in subdir bileşeni `<ref>:<subdir>` biçiminde iki nokta
	// üst üste ile ayrılıyor ve bu karakter kümede yok.
	fullSHAPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

// validateGitSource, kaynak deposunu doğrular.
//
// allowedHosts executor'ın yapılandırmasından gelir, istekten DEĞİL.
func validateGitSource(src *panelyv1.GitSource, allowedHosts []string) error {
	if src == nil {
		return errors.New("source zorunludur")
	}
	if err := validateGitHost(src.GetHost(), allowedHosts); err != nil {
		return err
	}
	if err := validatePathSegment("owner", src.GetOwner()); err != nil {
		return err
	}
	if err := validatePathSegment("repo", src.GetRepo()); err != nil {
		return err
	}
	if sha := src.GetCommitSha(); !fullSHAPattern.MatchString(sha) {
		return fmt.Errorf(
			"commit_sha tam 40 haneli küçük harf onaltılık olmalı (%q) — "+
				"dal/etiket adı kabul edilmez: derleme tekrarlanabilir olmalı ve "+
				"iki nokta üst üste içeren bir referans git fragment'inde subdir "+
				"bileşeni açar", sha)
	}
	return nil
}

func validateGitHost(host string, allowed []string) error {
	switch {
	case host == "":
		return errors.New("host boş olamaz")
	case len(host) > maxHostLen:
		return fmt.Errorf("host çok uzun (%d bayt)", len(host))
	case !hostPattern.MatchString(host):
		return fmt.Errorf(
			"host geçersiz (%q) — şema, port, kullanıcı bilgisi veya yol içeremez", host)
	}

	// Beyaz liste ele geçirilmiş bir panelyd tarafından genişletilemez:
	// executor'ın bayrağından geliyor.
	for _, a := range allowed {
		if host == a {
			return nil
		}
	}
	return fmt.Errorf(
		"host izin verilenler arasında değil (%q); izinli: %s — "+
			"liste executor'ın -allow-git-host bayrağındadır, istekle değiştirilemez",
		host, strings.Join(allowed, ", "))
}

func validatePathSegment(field, v string) error {
	switch {
	case v == "":
		return fmt.Errorf("%s boş olamaz", field)
	case !pathSegPattern.MatchString(v):
		return fmt.Errorf(
			"%s geçersiz (%q) — eğik çizgi, iki nokta ve boşluk temsil edilemez", field, v)
	case v == "." || v == "..":
		return fmt.Errorf("%s %q olamaz", field, v)
	}
	return nil
}

// validateDockerfilePath, depo köküne göreli Dockerfile yolunu doğrular.
//
// Boş kabul edilir ve "Dockerfile" anlamına gelir. `path` kullanılıyor,
// `filepath` DEĞİL: bu yol konteyner/depo tarafıdır ve geliştirme
// Windows'ta yapılıyor — filepath `C:\evil`'i sessizce geçirirdi.
func validateDockerfilePath(p string) error {
	if p == "" {
		return nil
	}
	switch {
	case len(p) > maxDockerfileLen:
		return fmt.Errorf("dockerfile_path çok uzun (%d bayt)", len(p))
	case path.IsAbs(p):
		return fmt.Errorf("dockerfile_path göreli olmalı (%q)", p)
	case path.Clean(p) != p:
		return fmt.Errorf("dockerfile_path temiz değil (%q, beklenen %q)", p, path.Clean(p))
	case p == "." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("dockerfile_path depo kökünün dışına çıkamaz (%q)", p)
	case strings.ContainsRune(p, '\\'):
		return fmt.Errorf("dockerfile_path ters eğik çizgi içeremez (%q)", p)
	}
	return nil
}

// validateImageBuild, ImageBuild isteğinin tamamını doğrular.
func validateImageBuild(req *panelyv1.ImageBuildRequest, allowedHosts []string) error {
	if req == nil {
		return errors.New("istek boş olamaz")
	}
	if err := validateReleaseRef(req.GetRelease()); err != nil {
		return err
	}
	if err := validateGitSource(req.GetSource(), allowedHosts); err != nil {
		return err
	}
	if err := validateDockerfilePath(req.GetDockerfilePath()); err != nil {
		return err
	}
	// Derleme argümanları ortam değişkenleriyle aynı kısıtlara tabi:
	// aynı execve argüman dizisine ve aynı NUL sorununa dokunuyorlar.
	if len(req.GetBuildArgs()) > maxBuildArgs {
		return fmt.Errorf("çok fazla derleme argümanı (%d, sınır %d)",
			len(req.GetBuildArgs()), maxBuildArgs)
	}
	return validateEnv(req.GetBuildArgs())
}

// BuildContextURL, doğrulanmış parçalardan BuildKit'e verilecek uzak
// bağlam URL'ini KURAR.
//
// Bu fonksiyonun var olma sebebi, hiçbir yerde bir URL'in ALINMAMASIDIR.
// Şema sabit https; kullanıcı bilgisi yok; fragment doğrulanmış 40 haneli
// bir sha olduğu için subdir bileşeni oluşturulamaz.
//
// Çağırmadan ÖNCE validateGitSource geçmiş olmalıdır.
func BuildContextURL(src *panelyv1.GitSource) string {
	return fmt.Sprintf("https://%s/%s/%s.git#%s",
		src.GetHost(), src.GetOwner(), src.GetRepo(), src.GetCommitSha())
}

// ImageTag, imaj etiketini kurar. Etiket de istekten alınmaz.
func ImageTag(appID, commitSHA string) string {
	return fmt.Sprintf("panely/%s:%s", appID, commitSHA)
}

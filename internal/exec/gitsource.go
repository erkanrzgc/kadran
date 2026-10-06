package exec

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
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
// allowedHosts ve allowedRepos executor'ın yapılandırmasından gelir,
// istekten DEĞİL.
func validateGitSource(src *kadranv1.GitSource, allowedHosts, allowedRepos []string) error {
	if src == nil {
		return errors.New("source is required")
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
	// Beyaz liste, hostta duran git kimlik bilgisinin ERİŞİMİNİ sınırlar:
	// onsuz, token'ın gördüğü her özel depo ImageBuild ile okunabilirdi
	// (repoallow.go'daki saldırı anlatımı).
	if !repoAllowed(src.GetOwner(), src.GetRepo(), allowedRepos) {
		return fmt.Errorf(
			"repository not on the allowlist: %s/%s — the executor only builds "+
				"allowed repositories", src.GetOwner(), src.GetRepo())
	}
	if sha := src.GetCommitSha(); !fullSHAPattern.MatchString(sha) {
		return fmt.Errorf(
			"commit_sha must be exactly 40 lowercase hex digits (%q) — "+
				"branch/tag names are not accepted: the build must be repeatable and "+
				"a ref containing a colon opens a subdir component in the git "+
				"fragment", sha)
	}
	return nil
}

func validateGitHost(host string, allowed []string) error {
	switch {
	case host == "":
		return errors.New("host must not be empty")
	case len(host) > maxHostLen:
		return fmt.Errorf("host too long (%d bytes)", len(host))
	case !hostPattern.MatchString(host):
		return fmt.Errorf(
			"invalid host (%q) — must not contain a scheme, port, user info or path", host)
	}

	// Beyaz liste ele geçirilmiş bir kadrand tarafından genişletilemez:
	// executor'ın bayrağından geliyor.
	for _, a := range allowed {
		if host == a {
			return nil
		}
	}
	return fmt.Errorf(
		"host not allowed (%q); allowed: %s — "+
			"the list is the executor's -allow-git-host flag and cannot be changed by a request",
		host, strings.Join(allowed, ", "))
}

func validatePathSegment(field, v string) error {
	switch {
	case v == "":
		return fmt.Errorf("%s must not be empty", field)
	case !pathSegPattern.MatchString(v):
		return fmt.Errorf(
			"invalid %s (%q) — slashes, colons and spaces cannot be represented", field, v)
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
		return fmt.Errorf("dockerfile_path too long (%d bytes)", len(p))
	case path.IsAbs(p):
		return fmt.Errorf("dockerfile_path must be relative (%q)", p)
	case path.Clean(p) != p:
		return fmt.Errorf("dockerfile_path is not clean (%q, expected %q)", p, path.Clean(p))
	case p == "." || strings.HasPrefix(p, "../"):
		return fmt.Errorf("dockerfile_path must not leave the repository root (%q)", p)
	case strings.ContainsRune(p, '\\'):
		return fmt.Errorf("dockerfile_path must not contain a backslash (%q)", p)
	}
	return nil
}

// validateImageBuild, ImageBuild isteğinin tamamını doğrular.
func validateImageBuild(req *kadranv1.ImageBuildRequest, allowedHosts, allowedRepos []string) error {
	if req == nil {
		return errors.New("request must not be empty")
	}
	if err := validateReleaseRef(req.GetRelease()); err != nil {
		return err
	}
	if err := validateGitSource(req.GetSource(), allowedHosts, allowedRepos); err != nil {
		return err
	}
	if err := validateDockerfilePath(req.GetDockerfilePath()); err != nil {
		return err
	}
	// Derleme argümanları ortam değişkenleriyle aynı kısıtlara tabi:
	// aynı execve argüman dizisine ve aynı NUL sorununa dokunuyorlar.
	if len(req.GetBuildArgs()) > maxBuildArgs {
		return fmt.Errorf("too many build args (%d, limit %d)",
			len(req.GetBuildArgs()), maxBuildArgs)
	}
	return validateEnv(req.GetBuildArgs(), maxEnvBytes)
}

// BuildContextURL, doğrulanmış parçalardan BuildKit'e verilecek uzak
// bağlam URL'ini KURAR.
//
// Bu fonksiyonun var olma sebebi, hiçbir yerde bir URL'in ALINMAMASIDIR.
// Şema sabit https; kullanıcı bilgisi yok; fragment doğrulanmış 40 haneli
// bir sha olduğu için subdir bileşeni oluşturulamaz.
//
// Çağırmadan ÖNCE validateGitSource geçmiş olmalıdır.
func BuildContextURL(src *kadranv1.GitSource) string {
	return fmt.Sprintf("https://%s/%s/%s.git#%s",
		src.GetHost(), src.GetOwner(), src.GetRepo(), src.GetCommitSha())
}

// Etiketi kuran fonksiyon BURADA DEĞİL, dockerdrv.ImageTag'dedir.
//
// Bir dönem burada da bir kopyası vardı. İki tanım, adı üreten iki ayrı
// yer demektir; biri değişip diğeri değişmediğinde derleme bir etiketle
// yapılır, konteyner başka bir etiketi arar ve arıza derleme anında değil
// ÇALIŞTIRMA anında ortaya çıkar. Tek tanım bu sınıfı kapatıyor.

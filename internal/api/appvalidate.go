package api

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// ── Doğrulama: KARAKTER KÜMELERİ evet, POLİTİKA hayır ────────────────
//
// Buradaki desenler exec.proto'da sözleşme olarak yazılı olanların
// aynısıdır ve executor'ın doğrulayıcılarında bir kez daha duruyor.
// Kopya KASITLI; gerekçesi göç 0002'nin başındaki notta ayrıntılı:
// sapma her iki yönde de yalnızca REDDE yol açar, hiçbir yönde bir
// kaçış üretmez. Asıl otorite executor'dır.
//
// ⚠ Buradaki kopya ImageTag'in silinen kopyasıyla AYNI SINIF DEĞİLDİR.
// Orada iki tanım, derlemenin bir etiketle yapılıp konteynerin başka bir
// etiketi aramasına yol açıyordu: SESSİZ bir çalışma-zamanı uyuşmazlığı.
// Burada en kötü sonuç, geçerli bir isteğin reddedilmesidir — gürültülü
// ve hemen görülür.
//
// POLİTİKA burada YOK. İzinli git host listesi executor'ın
// `-allow-git-host` bayrağındadır ve daemon onu BİLMEMELİDİR: liste bir
// işletme kararıdır ve ele geçirilmiş bir panelyd ona ekleme yapamamalı.
// Daemon yalnızca "bu bir host adına benziyor mu" der.

var (
	appIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	hostPattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	pathSegPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	fullSHAPattern  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	buildArgPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	branchPattern   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,254}$`)
)

const (
	maxReplicas     = 64
	maxBuildArgs    = 100
	minBlkioWeight  = 10
	maxBlkioWeight  = 1000
	maxDomainLen    = 253
	maxHealthPath   = 512
	maxDockerfile   = 4096
	maxBuildArgLen  = 32 << 10
	maxAppsPerQuery = 500
)

// validateAppSpec, uygulama tanımının tamamını doğrular.
func validateAppSpec(spec *panelyv1.AppSpec) error {
	if spec == nil {
		return errors.New("uygulama tanımı zorunludur")
	}
	if !appIDPattern.MatchString(spec.GetAppId()) {
		return fmt.Errorf(
			"app_id geçersiz (%q) — küçük harfle başlamalı; yalnızca küçük harf, "+
				"rakam ve tire; en fazla 32 karakter", spec.GetAppId())
	}
	if err := validateSource(spec); err != nil {
		return err
	}
	if err := validateDockerfilePath(spec.GetDockerfilePath()); err != nil {
		return err
	}
	if err := validateBuildArgs(spec.GetBuildArgs()); err != nil {
		return err
	}
	if p := spec.GetContainerPort(); p == 0 || p > 65535 {
		return fmt.Errorf("container_port 1-65535 arasında olmalı (%d)", p)
	}
	if r := spec.GetReplicas(); r == 0 || r > maxReplicas {
		return fmt.Errorf("replicas 1-%d arasında olmalı (%d) — "+
			"sıfır replika bir uygulama değil, silinmiş bir uygulamadır", maxReplicas, r)
	}
	if err := validateHealthPath(spec.GetHealthPath()); err != nil {
		return err
	}
	if err := validateDomain(spec.GetDomain()); err != nil {
		return err
	}
	return validateLimits(spec.GetLimits())
}

func validateSource(spec *panelyv1.AppSpec) error {
	host := spec.GetGitHost()
	switch {
	case host == "":
		return errors.New("git_host boş olamaz")
	case len(host) > maxDomainLen:
		return fmt.Errorf("git_host çok uzun (%d bayt)", len(host))
	case !hostPattern.MatchString(host):
		return fmt.Errorf("git_host geçersiz (%q) — şema, port veya yol içeremez", host)
	}

	for _, f := range []struct{ name, value string }{
		{"git_owner", spec.GetGitOwner()},
		{"git_repo", spec.GetGitRepo()},
	} {
		if !pathSegPattern.MatchString(f.value) || f.value == "." || f.value == ".." {
			return fmt.Errorf("%s geçersiz (%q) — eğik çizgi ve iki nokta temsil edilemez",
				f.name, f.value)
		}
	}

	// Dal sunucu tarafında hiçbir yere geçirilmiyor (istemci onu commit'e
	// çözüyor), ama veritabanına yazılıyor: biçimsiz bir değer sonradan
	// istemcinin çözümünü bozar.
	if b := spec.GetGitBranch(); !branchPattern.MatchString(b) {
		return fmt.Errorf("git_branch geçersiz (%q)", b)
	}
	return nil
}

// validateDockerfilePath, depo köküne göreli yolu doğrular.
//
// `path` kullanılıyor, `filepath` DEĞİL. Bu yol DEPO tarafındadır ve
// geliştirme Windows'ta yapılıyor: filepath, Windows'ta `C:\evil`i
// "göreli" sayıp sessizce geçirirdi.
func validateDockerfilePath(p string) error {
	if p == "" {
		return nil // "Dockerfile" anlamına gelir.
	}
	switch {
	case len(p) > maxDockerfile:
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

func validateBuildArgs(args map[string]string) error {
	if len(args) > maxBuildArgs {
		return fmt.Errorf("çok fazla derleme argümanı (%d, sınır %d)", len(args), maxBuildArgs)
	}
	for k, v := range args {
		if !buildArgPattern.MatchString(k) {
			return fmt.Errorf("derleme argümanı adı geçersiz (%q)", k)
		}
		if len(v) > maxBuildArgLen {
			return fmt.Errorf("derleme argümanı %q çok uzun (%d bayt)", k, len(v))
		}
		// NUL, execve argüman dizisinde dizeyi keser: kabul edilen değer
		// ile çalışan değer ayrışırdı.
		if strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("derleme argümanı %q NUL baytı içeriyor", k)
		}
	}
	return nil
}

func validateHealthPath(p string) error {
	switch {
	case p == "":
		return nil
	case len(p) > maxHealthPath:
		return fmt.Errorf("health_path çok uzun (%d bayt)", len(p))
	case !strings.HasPrefix(p, "/"):
		return fmt.Errorf("health_path eğik çizgiyle başlamalı (%q)", p)
	case strings.ContainsAny(p, " \t\r\n"):
		// Boşluk ve satır sonu, kurulacak HTTP istek satırını bölebilir.
		return fmt.Errorf("health_path boşluk veya satır sonu içeremez (%q)", p)
	}
	return nil
}

func validateDomain(d string) error {
	switch {
	case d == "":
		return nil // Alan adı olmayan uygulama geçerli: yalnızca iç ağdan erişilir.
	case len(d) > maxDomainLen:
		return fmt.Errorf("domain çok uzun (%d bayt)", len(d))
	case !hostPattern.MatchString(d):
		return fmt.Errorf("domain geçersiz (%q) — şema, port veya yol içeremez", d)
	}
	return nil
}

// validateLimits, kaynak kotalarını doğrular. Sıfır KABUL EDİLMEZ:
// limitsiz bir konteyner, tek sunucudaki diğer her şeyi aç bırakabilir.
func validateLimits(l *panelyv1.ResourceLimits) error {
	if l == nil {
		return errors.New("limits zorunludur — limitsiz konteyner yoktur")
	}
	if l.GetMemoryBytes() == 0 {
		return errors.New("limits.memory_bytes sıfır olamaz")
	}
	if l.GetCpuMillis() == 0 {
		return errors.New("limits.cpu_millis sıfır olamaz")
	}
	if w := l.GetBlkioWeight(); w < minBlkioWeight || w > maxBlkioWeight {
		return fmt.Errorf("limits.blkio_weight %d-%d arasında olmalı (%d)",
			minBlkioWeight, maxBlkioWeight, w)
	}
	return nil
}

// validateCommitSHA, dağıtım hedefini doğrular.
//
// Kısa sha ve dal adı KABUL EDİLMEZ. İki sebep exec.proto'da yazılı:
// dal hareket eder (geri alma tekrarlanabilirliğe dayanır) ve iki nokta
// üst üste içeren bir referans, git fragment'inde subdir bileşeni açar.
// 40 haneli hex ikisini de temsil edilemez kılar.
func validateCommitSHA(sha string) error {
	if !fullSHAPattern.MatchString(sha) {
		return fmt.Errorf(
			"commit_sha tam 40 haneli küçük harf onaltılık olmalı (%q) — "+
				"dal veya etiket adı kabul edilmez", sha)
	}
	return nil
}

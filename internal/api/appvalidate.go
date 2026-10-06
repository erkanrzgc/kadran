package api

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
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
// işletme kararıdır ve ele geçirilmiş bir kadrand ona ekleme yapamamalı.
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

	// ── env sınırları: executor'ınkiyle AYNI olmak ZORUNDA ──────────
	//
	// Bu üç sabit internal/exec/validate.go'daki maxEnvEntries,
	// maxEnvBytes ve maxEnvKeyBytes ile birebir aynıdır. Kopya olmaları
	// kasıtlı: kadrand ayrıcalıklı paketi içe aktarmıyor ve aktarmamalı
	// (yetki sınırı ikilinin kendisinde, ortak bir kütüphanede değil).
	//
	// ⚠ Buradaki sınır executor'ınkinden GEVŞEK OLAMAZ. Gevşek olsaydı
	// `app create` tanımı kabul eder, ilk `deploy` executor tarafından
	// reddedilirdi: kullanıcı geçerli sandığı bir kayıtla kalır ve hatayı
	// sebebinden günler sonra, tamamen başka bir komutta görür.
	//
	// ⚠ Sınır TOPLAM bayt üzerinden, DEĞER BAŞINA değil. Hemen yukarıdaki
	// maxBuildArgLen değer başına işliyor ve o kalıbı buraya kopyalamak
	// tam da yukarıdaki tuzağı kurardı: 200 × 32 KiB burada geçer,
	// executor'da çakılır.
	maxEnvEntries  = 200
	maxEnvBytes    = 32 << 10
	maxEnvKeyBytes = 256
)

// validateAppSpec, uygulama tanımının tamamını doğrular.
func validateAppSpec(spec *kadranv1.AppSpec) error {
	if spec == nil {
		return errors.New("app spec is required")
	}
	if !appIDPattern.MatchString(spec.GetAppId()) {
		return fmt.Errorf(
			"invalid app_id (%q) — must start with a lowercase letter; only lowercase letters, "+
				"digits and hyphens; at most 32 characters", spec.GetAppId())
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
	if err := validateEnv(spec.GetEnv()); err != nil {
		return err
	}
	if err := validateVolumes(spec.GetVolumes()); err != nil {
		return err
	}
	if p := spec.GetContainerPort(); p == 0 || p > 65535 {
		return fmt.Errorf("container_port must be between 1 and 65535 (%d)", p)
	}
	if r := spec.GetReplicas(); r == 0 || r > maxReplicas {
		return fmt.Errorf("replicas must be between 1 and %d (%d) — "+
			"an app with zero replicas is not an app, it is a deleted app", maxReplicas, r)
	}
	if err := validateHealthPath(spec.GetHealthPath()); err != nil {
		return err
	}
	if err := validateDomain(spec.GetDomain()); err != nil {
		return err
	}
	return validateLimits(spec.GetLimits())
}

func validateSource(spec *kadranv1.AppSpec) error {
	host := spec.GetGitHost()
	switch {
	case host == "":
		return errors.New("git_host must not be empty")
	case len(host) > maxDomainLen:
		return fmt.Errorf("git_host too long (%d bytes)", len(host))
	case !hostPattern.MatchString(host):
		return fmt.Errorf("invalid git_host (%q) — must not contain a scheme, port or path", host)
	}

	for _, f := range []struct{ name, value string }{
		{"git_owner", spec.GetGitOwner()},
		{"git_repo", spec.GetGitRepo()},
	} {
		if !pathSegPattern.MatchString(f.value) || f.value == "." || f.value == ".." {
			return fmt.Errorf("invalid %s (%q) — slashes and colons cannot be represented",
				f.name, f.value)
		}
	}

	// Dal sunucu tarafında hiçbir yere geçirilmiyor (istemci onu commit'e
	// çözüyor), ama veritabanına yazılıyor: biçimsiz bir değer sonradan
	// istemcinin çözümünü bozar.
	if b := spec.GetGitBranch(); !branchPattern.MatchString(b) {
		return fmt.Errorf("invalid git_branch (%q)", b)
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

func validateBuildArgs(args map[string]string) error {
	if len(args) > maxBuildArgs {
		return fmt.Errorf("too many build args (%d, limit %d)", len(args), maxBuildArgs)
	}
	for k, v := range args {
		if !buildArgPattern.MatchString(k) {
			return fmt.Errorf("invalid build arg name (%q)", k)
		}
		if len(v) > maxBuildArgLen {
			return fmt.Errorf("build arg %q too long (%d bytes)", k, len(v))
		}
		// NUL, execve argüman dizisinde dizeyi keser: kabul edilen değer
		// ile çalışan değer ayrışırdı.
		if strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("build arg %q contains a NUL byte", k)
		}
	}
	return nil
}

// validateEnv, ortam değişkenlerini doğrular.
//
// Anahtar deseni `build_args` ile aynı (`buildArgPattern`) ve bu tesadüf
// değil: ikisi de kabuk değişkeni adı kuralına uyuyor ve executor her
// ikisi için de aynı deseni kullanıyor. Ayrı bir desen tanımlamak, iki
// tarafın bir gün sessizce ayrışmasına davetiye olurdu.
//
// ⚠ Değer İÇERİĞİ doğrulanmıyor ve bu kasıtlı: env değeri keyfi metindir
// (URL, JSON, base64, çok satırlı sertifika). Bir "makul değer" tanımı
// uydurmak, geçerli kullanımları reddeder ve kullanıcıyı doğrulamayı
// atlatmanın yollarını aramaya iterdi. Güvenlik burada içerikten değil,
// değerin ASLA kabuk tarafından yorumlanmamasından geliyor: executor
// haritayı doğrudan Docker'a KEY=VALUE dizisi olarak veriyor, araya kabuk
// girmiyor.
func validateEnv(env map[string]string) error {
	if len(env) > maxEnvEntries {
		return fmt.Errorf("too many environment variables (%d, limit %d)",
			len(env), maxEnvEntries)
	}
	total := 0
	for k, v := range env {
		if len(k) > maxEnvKeyBytes {
			return fmt.Errorf("environment variable name too long (%d bytes, limit %d)",
				len(k), maxEnvKeyBytes)
		}
		if !buildArgPattern.MatchString(k) {
			return fmt.Errorf("invalid environment variable name (%q) — "+
				"must match ^[A-Za-z_][A-Za-z0-9_]*$", k)
		}
		// NUL, değişkeni execve dizisinde KESER: kabul edilen değer ile
		// konteynerde görünen değer ayrışırdı.
		if strings.ContainsRune(k, 0) || strings.ContainsRune(v, 0) {
			return fmt.Errorf("environment variable %q contains a NUL byte", k)
		}
		total += len(k) + len(v) + 1 // +1: "KEY=VALUE" içindeki eşittir
	}
	if total > maxEnvBytes {
		return fmt.Errorf("environment variables total %d bytes, limit %d "+
			"— the limit applies to the TOTAL, not per value", total, maxEnvBytes)
	}
	return nil
}

// validateEnvRemove, ayarlama ile silmenin ÇELİŞMEDİĞİNİ doğrular.
//
// Aynı anahtarı hem ayarlayıp hem silmek iki zıt niyet taşır. Sessizce
// birini seçmek, hangisinin uygulandığını kullanıcı için belirsiz
// bırakırdı — ve o belirsizlik, haritanın gezilme sırasına bağlı bir
// hataya dönüşürdü. Reddetmek, kullanıcıyı ne istediğini söylemeye
// zorluyor.
func validateEnvRemove(set map[string]string, remove []string) error {
	for _, k := range remove {
		if !buildArgPattern.MatchString(k) {
			return fmt.Errorf("invalid name of environment variable to remove (%q)", k)
		}
		if _, both := set[k]; both {
			return fmt.Errorf("%q is both set and removed — "+
				"say which one you want", k)
		}
	}
	return nil
}

func validateHealthPath(p string) error {
	switch {
	case p == "":
		return nil
	case len(p) > maxHealthPath:
		return fmt.Errorf("health_path too long (%d bytes)", len(p))
	case !strings.HasPrefix(p, "/"):
		return fmt.Errorf("health_path must start with a slash (%q)", p)
	case strings.ContainsAny(p, " \t\r\n"):
		// Boşluk ve satır sonu, kurulacak HTTP istek satırını bölebilir.
		return fmt.Errorf("health_path must not contain whitespace or line breaks (%q)", p)
	}
	return nil
}

func validateDomain(d string) error {
	switch {
	case d == "":
		return nil // Alan adı olmayan uygulama geçerli: yalnızca iç ağdan erişilir.
	case len(d) > maxDomainLen:
		return fmt.Errorf("domain too long (%d bytes)", len(d))
	case !hostPattern.MatchString(d):
		return fmt.Errorf("invalid domain (%q) — must not contain a scheme, port or path", d)
	}
	return nil
}

// validateLimits, kaynak kotalarını doğrular. Sıfır KABUL EDİLMEZ:
// limitsiz bir konteyner, tek sunucudaki diğer her şeyi aç bırakabilir.
func validateLimits(l *kadranv1.ResourceLimits) error {
	if l == nil {
		return errors.New("limits are required — there is no container without limits")
	}
	if l.GetMemoryBytes() == 0 {
		return errors.New("limits.memory_bytes must not be zero")
	}
	if l.GetCpuMillis() == 0 {
		return errors.New("limits.cpu_millis must not be zero")
	}
	if w := l.GetBlkioWeight(); w < minBlkioWeight || w > maxBlkioWeight {
		return fmt.Errorf("limits.blkio_weight must be between %d and %d (%d)",
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
			"commit_sha must be exactly 40 lowercase hex digits (%q) — "+
				"branch or tag names are not accepted", sha)
	}
	return nil
}

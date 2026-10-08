package exec

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Şema ne kadar dar olursa olsun, tellerden geçen bayt dizisi keyfîdir.
// Buradaki doğrulayıcılar şemanın GRAMERİNİ zorlar: proto3 "bu string
// ^[a-z][a-z0-9-]*$ olmalı" diyemez, dolayısıyla o kısım koddadır.
//
// Kural: doğrulama, ayrıcalıklı hiçbir şey yapılmadan ÖNCE ve tek noktada
// olur. Sürücü katmanı doğrulanmamış bir alan görmez.

const (
	maxAppIDLen     = 32
	maxReleaseIDLen = 64
	maxVolumeName   = 64

	// maxReplica, bir sürümün replika sayısının üst sınırıdır. Güvenlik
	// sınırı değil, akıl sağlığı sınırı: replica alanı uint32 ve dağıtım
	// mantığı her replika için konteyner oluşturuyor.
	maxReplica = 64

	maxVolumes   = 16
	maxMountPath = 4096 // PATH_MAX

	// maxStopTimeoutSeconds, SIGTERM ile SIGKILL arasındaki en uzun
	// bekleme. Sınırsız bırakılsaydı kadrand, executor'ın bir işleyicisini
	// istediği kadar meşgul tutabilirdi — yavaş bir kaynak tüketimi yolu.
	maxStopTimeoutSeconds = 300
	maxEnvEntries         = 200
	maxEnvBytes           = 32 << 10
	maxEnvKeyBytes        = 256

	// Kaynak limitleri. Alt sınırlar "limitsiz konteyner yok" kuralını
	// zorlar; üst sınırlar Docker API'sine saçma değer gitmesini önler.
	minMemoryBytes = 6 << 20 // Docker'ın kendi alt sınırı
	maxMemoryBytes = 1 << 40
	minCPUMillis   = 10
	maxCPUMillis   = 64_000
	minBlkioWeight = 10
	maxBlkioWeight = 1000
)

var (
	// Karakter kümeleri nokta ve eğik çizgi İÇERMEZ. Bu tesadüf değil:
	// bu değerlerden yol ve Docker etiketi üretiliyor, dolayısıyla
	// `..` ve `/` temsil edilemez olmalı — filtrelenmesi değil.
	appIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)
	releaseIDPattern = regexp.MustCompile(`^[a-z0-9]{1,64}$`)
	volumeNamePat    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	commitSHAPattern = regexp.MustCompile(`^[0-9a-f]{7,64}$`)
	envKeyPattern    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// forbiddenMountRoots, konteyner içinde üzerine bağlanamayacak çekirdek
// arayüzleridir. Bunların üstüne bağlamak, süreç görünürlüğünü ve aygıt
// düğümlerini uygulamanın kontrolüne verir.
var forbiddenMountRoots = []string{"/proc", "/sys", "/dev"}

// ── Adresleme ────────────────────────────────────────────────────────

func validateAppID(appID string) error {
	if appID == "" {
		return errors.New("app_id must not be empty")
	}
	if !appIDPattern.MatchString(appID) {
		return fmt.Errorf("invalid app_id format (%q): ^[a-z][a-z0-9-]{0,%d}$",
			appID, maxAppIDLen-1)
	}
	return nil
}

func validateReleaseRef(ref *kadranv1.ReleaseRef) error {
	if ref == nil {
		return errors.New("a release reference is required")
	}
	if err := validateAppID(ref.GetAppId()); err != nil {
		return err
	}
	if !releaseIDPattern.MatchString(ref.GetReleaseId()) {
		return fmt.Errorf("invalid release_id format (%q): ^[a-z0-9]{1,%d}$",
			ref.GetReleaseId(), maxReleaseIDLen)
	}
	return nil
}

func validateContainerRef(ref *kadranv1.ContainerRef) error {
	if ref == nil {
		return errors.New("a container reference is required")
	}
	if err := validateReleaseRef(ref.GetRelease()); err != nil {
		return err
	}
	if ref.GetReplica() >= maxReplica {
		return fmt.Errorf("replica %d, limit %d", ref.GetReplica(), maxReplica)
	}
	return nil
}

func validateSelector(sel *kadranv1.ContainerSelector) error {
	if sel == nil {
		return errors.New("a selector is required")
	}
	if err := validateReleaseRef(sel.GetRelease()); err != nil {
		return err
	}
	// Replica VERİLMEMİŞ olabilir; o zaman sürümün tamamı seçilir.
	// Verilmişse sınır içinde olmalı.
	if sel.Replica != nil && sel.GetReplica() >= maxReplica {
		return fmt.Errorf("replica %d, limit %d", sel.GetReplica(), maxReplica)
	}
	return nil
}

func validateStopTimeout(seconds uint32) error {
	if seconds > maxStopTimeoutSeconds {
		return fmt.Errorf("timeout_seconds %d, limit %d seconds",
			seconds, maxStopTimeoutSeconds)
	}
	return nil
}

// ── Konteyner oluşturma ──────────────────────────────────────────────

func validateCreate(req *kadranv1.ContainerCreateRequest) error {
	if req == nil {
		return errors.New("empty request")
	}
	if err := validateContainerRef(req.GetRef()); err != nil {
		return err
	}
	if !commitSHAPattern.MatchString(req.GetCommitSha()) {
		return fmt.Errorf("invalid commit_sha format (%q): ^[0-9a-f]{7,64}$",
			req.GetCommitSha())
	}
	if err := validateEnv(req.GetEnv(), maxSealedEnvBytes); err != nil {
		return err
	}
	if err := validateLimits(req.GetLimits()); err != nil {
		return err
	}
	if err := validateVolumes(req.GetVolumes()); err != nil {
		return err
	}
	if p := req.GetContainerPort(); p == 0 || p > 65535 {
		return fmt.Errorf("container_port %d, must be in 1-65535", p)
	}
	return nil
}

func validateEnv(env map[string]string, maxTotal int) error {
	if len(env) > maxEnvEntries {
		return fmt.Errorf("env has %d entries, limit %d", len(env), maxEnvEntries)
	}
	total := 0
	for k, v := range env {
		if len(k) > maxEnvKeyBytes {
			return fmt.Errorf("env key too long (%d bytes)", len(k))
		}
		if !envKeyPattern.MatchString(k) {
			return fmt.Errorf("invalid env key (%q): ^[A-Za-z_][A-Za-z0-9_]*$", k)
		}
		// NUL, execve'nin argüman dizisini erken sonlandırır: değerin
		// görünen kısmı ile çekirdeğe ulaşan kısmı ayrışabilir.
		if strings.ContainsRune(v, 0) || strings.ContainsRune(k, 0) {
			return fmt.Errorf("env %q contains a NUL byte", k)
		}
		total += len(k) + len(v)
	}
	if total > maxTotal {
		return fmt.Errorf("env total %d bytes, limit %d", total, maxTotal)
	}
	return nil
}

func validateLimits(l *kadranv1.ResourceLimits) error {
	// Limitsiz konteyner yoktur: tek sunucuda kaynak tüketen bir uygulama
	// kadrand dahil her şeyi düşürebilir.
	if l == nil {
		return errors.New("resource limits are required")
	}
	if m := l.GetMemoryBytes(); m < minMemoryBytes || m > maxMemoryBytes {
		return fmt.Errorf("memory_bytes %d, must be in %d-%d",
			m, minMemoryBytes, maxMemoryBytes)
	}
	if c := l.GetCpuMillis(); c < minCPUMillis || c > maxCPUMillis {
		return fmt.Errorf("cpu_millis %d, must be in %d-%d",
			c, minCPUMillis, maxCPUMillis)
	}
	if w := l.GetBlkioWeight(); w < minBlkioWeight || w > maxBlkioWeight {
		return fmt.Errorf("blkio_weight %d, must be in %d-%d",
			w, minBlkioWeight, maxBlkioWeight)
	}
	return nil
}

func validateVolumes(vols []*kadranv1.VolumeMount) error {
	if len(vols) > maxVolumes {
		return fmt.Errorf("%d volumes, limit %d", len(vols), maxVolumes)
	}
	seen := make([]string, 0, len(vols))
	for i, v := range vols {
		if v == nil {
			return fmt.Errorf("volume %d is empty", i)
		}
		if !volumeNamePat.MatchString(v.GetVolumeName()) {
			return fmt.Errorf("invalid volume_name format (%q): ^[a-z0-9][a-z0-9-]{0,%d}$",
				v.GetVolumeName(), maxVolumeName-1)
		}
		mp := v.GetMountPath()
		if err := validateMountPath(mp); err != nil {
			return fmt.Errorf("hacim %q: %w", v.GetVolumeName(), err)
		}
		// İç içe geçen bağlama noktaları, hangi hacmin görüneceğini
		// bağlama sırasına bırakır — belirsiz davranış güvenlik sınırında
		// kabul edilemez.
		for _, prev := range seen {
			if pathOverlaps(prev, mp) {
				return fmt.Errorf("mount_path %q overlaps %q", prev, mp)
			}
		}
		seen = append(seen, mp)
	}
	return nil
}

// validateMountPath, konteyner İÇİNDEKİ bağlama noktasını denetler.
//
// Host yolu hiçbir yerde alınmadığı için buradaki kontroller host'u
// korumaz; konteyner içinde tutarlı ve öngörülebilir bir dosya sistemi
// sağlarlar.
func validateMountPath(p string) error {
	switch {
	case p == "":
		return errors.New("mount_path must not be empty")
	case len(p) > maxMountPath:
		return fmt.Errorf("mount_path too long (%d bytes)", len(p))
	case !path.IsAbs(p):
		return fmt.Errorf("mount_path must be absolute (%q)", p)
	// Temizlik kontrolü `..` denetimini de kapsar: path.Clean("/a/../b")
	// "/b" döner, yani girdiden farklıdır. Ayrıca "//a", "/a/./b" ve
	// "/a/" gibi AYNI yeri gösteren farklı yazımları da eler — bunlar
	// elenmezse aşağıdaki çakışma kontrolü atlatılabilirdi.
	case path.Clean(p) != p:
		return fmt.Errorf("mount_path is not clean (%q, expected %q)", p, path.Clean(p))
	case p == "/":
		return errors.New("mount_path must not be the root directory")
	}
	for _, root := range forbiddenMountRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return fmt.Errorf("mount_path must not be under %q (%q)", root, p)
		}
	}
	return nil
}

// pathOverlaps, iki temiz mutlak yolun aynı ya da iç içe olup olmadığını
// söyler. Her iki yolun da path.Clean'den geçtiği varsayılır.
func pathOverlaps(a, b string) bool {
	return a == b ||
		strings.HasPrefix(a, b+"/") ||
		strings.HasPrefix(b, a+"/")
}

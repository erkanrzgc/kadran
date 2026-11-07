package guard

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/erkanrzgc/kadran/desktop/internal/jsonv"
)

// create / update yükünün iç içe alanları.
//
// KURAL: sunucunun kurallarını AYNEN yansıt, ASLA daha sıkı olma
// (internal/api/appvalidate.go ve volumevalidate.go). Daha sıkı bir kural
// meşru uygulamaları kilitler. Daha gevşek olması ise zararsız: asıl
// otorite sunucu ve executor'dır; burada amaç, ele geçmiş bir arayüzün
// onay kutusuna yanlış şey yazdırmasını ve argüman enjeksiyonu biçimlerini
// kesmektir. Protojson bilinmeyen alanı reddettiği için burada da
// bilinmeyen alan reddedilir.

// Mode, yükün türü.
type Mode int

const (
	Create Mode = iota
	Update
)

const (
	maxPayloadBytes  = 512 * 1024
	maxReplicas      = 64
	maxBuildArgs     = 100
	maxBuildArgBytes = 32 << 10
	maxDomain        = 253
	maxHealthPath    = 512
	maxDockerfile    = 4096
	maxEnvEntries    = 200
	maxEnvBytes      = 32 << 10
	maxEnvKeyBytes   = 256
	maxVolumes       = 16
	maxMountPath     = 4096
	uint32Max        = math.MaxUint32
)

var (
	hostPattern     = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	pathSegment     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,99}$`)
	buildArgName    = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	volumeName      = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	decimalText     = regexp.MustCompile(`^\d{1,15}$`)
	decimalText20   = regexp.MustCompile(`^\d{1,20}$`)
	healthPathSpace = regexp.MustCompile(`[ \t\r\n]`)

	forbiddenMountRoots = []string{"/proc", "/sys", "/dev"}
	uint64Max           = new(big.Int).SetUint64(math.MaxUint64)

	createFields   = []string{"app_id", "git_host", "git_owner", "git_repo", "git_branch", "dockerfile_path", "build_args", "container_port", "replicas", "health_path", "domain", "limits", "env", "volumes"}
	updateFields   = []string{"domain", "git_branch", "health_path", "replicas", "env", "env_remove", "volumes", "volume_remove"}
	createOnlyKeys = []string{"app_id", "git_host", "git_owner", "git_repo", "dockerfile_path", "build_args", "container_port", "limits"}
	updateScalars  = []string{"domain", "git_branch", "health_path", "replicas"}
)

// AppPayload, create ya da update yükünü doğrular ve sırası korunmuş
// kopyasını döner: onay kutusu da sunucuya giden de bu kopyadır.
func AppPayload(raw json.RawMessage, mode Mode) (*jsonv.Object, error) {
	value, err := jsonv.Decode(raw)
	if err != nil {
		return nil, errors.New("app payload must be an object")
	}
	obj, ok := value.(*jsonv.Object)
	if !ok {
		return nil, errors.New("app payload must be an object")
	}
	fields := createFields
	if mode == Update {
		fields = updateFields
	}
	for _, key := range obj.Keys() {
		if !slices.Contains(fields, key) {
			return nil, errors.New("unknown app field")
		}
	}
	encoded, err := jsonv.Encode(obj)
	if err != nil || len(encoded) > maxPayloadBytes {
		return nil, errors.New("app payload is too large")
	}
	if mode == Create {
		id, _ := field(obj, "app_id").(string)
		if _, err := AppID(id); err != nil {
			return nil, err
		}
		return obj, validateCreateSpec(obj)
	}
	return obj, validateAppUpdate(obj)
}

func bad(format string, args ...any) error { return fmt.Errorf(format, args...) }

// field, anahtarın değeri; yoksa nil (JavaScript'te undefined).
func field(obj *jsonv.Object, key string) any {
	v, _ := obj.Get(key)
	return v
}

// str: protojson'daki gibi yok ya da null → boş metin.
func str(v any, name string) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", nil
	case string:
		return t, nil
	}
	return "", bad("%s must be a string", name)
}

// safeInteger, JSON sayısının JavaScript'teki Number.isSafeInteger karşılığı.
func safeInteger(n json.Number) (float64, bool) {
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil || math.IsInf(f, 0) || f != math.Trunc(f) || math.Abs(f) > maxSafeInteger {
		return 0, false
	}
	return f, true
}

// uintIn: protojson sayıyı JSON sayısı ya da onluk metin olarak kabul eder.
func uintIn(v any, name string, lo, hi float64) error {
	n := 0.0
	ok := true
	switch t := v.(type) {
	case nil:
	case string:
		if decimalText.MatchString(t) {
			n, _ = strconv.ParseFloat(t, 64)
		} else {
			ok = false
		}
	case json.Number:
		n, ok = safeInteger(t)
	default:
		ok = false
	}
	if !ok || n < lo || n > hi {
		return bad("%s must be an integer from %d to %d", name, int64(lo), int64(hi))
	}
	return nil
}

func uint64Positive(v any, name string) error {
	var n *big.Int
	switch t := v.(type) {
	case string:
		if decimalText20.MatchString(t) {
			n, _ = new(big.Int).SetString(t, 10)
		}
	case json.Number:
		if f, ok := safeInteger(t); ok && f >= 0 {
			n = big.NewInt(int64(f))
		}
	}
	if n == nil || n.Sign() == 0 || n.Cmp(uint64Max) > 0 {
		return bad("%s must be a positive integer", name)
	}
	return nil
}

func onlyKeys(obj *jsonv.Object, allowed []string, what string) error {
	for _, key := range obj.Keys() {
		if !slices.Contains(allowed, key) {
			return bad("unknown %s field", what)
		}
	}
	return nil
}

func checkDomain(v any, name string) error {
	d, err := str(v, name)
	if err != nil {
		return err
	}
	if d == "" {
		return nil
	}
	if len(d) > maxDomain || !hostPattern.MatchString(d) {
		return bad("invalid %s", name)
	}
	return nil
}

func checkHealthPath(v any) error {
	p, err := str(v, "health_path")
	if err != nil || p == "" {
		return err
	}
	if len(p) > maxHealthPath || !strings.HasPrefix(p, "/") || healthPathSpace.MatchString(p) {
		return errors.New("invalid health_path")
	}
	return nil
}

func checkBranch(v any) error {
	b, err := str(v, "git_branch")
	if err != nil {
		return err
	}
	if !BranchPattern.MatchString(b) {
		return errors.New("invalid git_branch")
	}
	return nil
}

func checkSource(spec *jsonv.Object) error {
	host, err := str(field(spec, "git_host"), "git_host")
	if err != nil {
		return err
	}
	if host == "" || len(host) > maxDomain || !hostPattern.MatchString(host) {
		return errors.New("invalid git_host")
	}
	for _, name := range []string{"git_owner", "git_repo"} {
		segment, err := str(field(spec, name), name)
		if err != nil {
			return err
		}
		if !pathSegment.MatchString(segment) || segment == "." || segment == ".." {
			return bad("invalid %s", name)
		}
	}
	return checkBranch(field(spec, "git_branch"))
}

func checkDockerfile(v any) error {
	p, err := str(v, "dockerfile_path")
	if err != nil || p == "" {
		return err
	}
	if len(p) > maxDockerfile || strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "." || strings.HasPrefix(p, "../") || strings.Contains(p, `\`) {
		return errors.New("invalid dockerfile_path")
	}
	return nil
}

func checkBuildArgs(v any) error {
	if v == nil {
		return nil
	}
	obj, ok := v.(*jsonv.Object)
	if !ok {
		return errors.New("build_args must be an object")
	}
	if obj.Len() > maxBuildArgs {
		return errors.New("too many build args")
	}
	for _, k := range obj.Keys() {
		if !buildArgName.MatchString(k) {
			return errors.New("invalid build arg name")
		}
		s, isText := field(obj, k).(string)
		if !isText || len(s) > maxBuildArgBytes || strings.Contains(s, "\x00") {
			return errors.New("invalid build arg value")
		}
	}
	return nil
}

func checkEnv(v any) (*jsonv.Object, error) {
	if v == nil {
		return jsonv.NewObject(), nil
	}
	obj, ok := v.(*jsonv.Object)
	if !ok {
		return nil, errors.New("env must be an object")
	}
	if obj.Len() > maxEnvEntries {
		return nil, errors.New("too many environment variables")
	}
	total := 0
	for _, k := range obj.Keys() {
		if len(k) > maxEnvKeyBytes || !buildArgName.MatchString(k) {
			return nil, errors.New("invalid environment variable name")
		}
		s, isText := field(obj, k).(string)
		if !isText || strings.Contains(s, "\x00") {
			return nil, errors.New("invalid environment variable value")
		}
		total += len(k) + len(s) + 1
	}
	if total > maxEnvBytes {
		return nil, errors.New("environment variables are too large")
	}
	return obj, nil
}

func checkMountPath(p string) error {
	if p == "" || len(p) > maxMountPath || !strings.HasPrefix(p, "/") || path.Clean(p) != p || p == "/" {
		return errors.New("invalid volume mount path")
	}
	for _, root := range forbiddenMountRoots {
		if p == root || strings.HasPrefix(p, root+"/") {
			return errors.New("invalid volume mount path")
		}
	}
	return nil
}

func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func checkVolumes(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, errors.New("volumes must be a list")
	}
	if len(list) > maxVolumes {
		return nil, errors.New("too many volumes")
	}
	var names, paths []string
	for _, item := range list {
		obj, ok := item.(*jsonv.Object)
		if !ok {
			return nil, errors.New("invalid volume")
		}
		if err := onlyKeys(obj, []string{"name", "mount_path", "read_only"}, "volume"); err != nil {
			return nil, err
		}
		name, err := str(field(obj, "name"), "volume name")
		if err != nil {
			return nil, err
		}
		if !volumeName.MatchString(name) || slices.Contains(names, name) {
			return nil, errors.New("invalid volume name")
		}
		switch field(obj, "read_only").(type) {
		case nil, bool:
		default:
			return nil, errors.New("volume read_only must be true or false")
		}
		mount, err := str(field(obj, "mount_path"), "volume mount path")
		if err != nil {
			return nil, err
		}
		if err := checkMountPath(mount); err != nil {
			return nil, err
		}
		for _, previous := range paths {
			if overlaps(previous, mount) {
				return nil, errors.New("volume mount points overlap")
			}
		}
		names = append(names, name)
		paths = append(paths, mount)
	}
	return names, nil
}

func checkNameList(v any, name string, pattern *regexp.Regexp, conflicts func(string) bool) error {
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return bad("%s must be a list", name)
	}
	for _, item := range list {
		s, isText := item.(string)
		if !isText || !pattern.MatchString(s) {
			return bad("invalid name in %s", name)
		}
		if conflicts(s) {
			return bad("%s conflicts with the values being set", name)
		}
	}
	return nil
}

func checkLimits(v any) error {
	obj, ok := v.(*jsonv.Object)
	if !ok {
		return errors.New("limits are required")
	}
	if err := onlyKeys(obj, []string{"memory_bytes", "cpu_millis", "blkio_weight"}, "limits"); err != nil {
		return err
	}
	if err := uint64Positive(field(obj, "memory_bytes"), "limits.memory_bytes"); err != nil {
		return err
	}
	if err := uintIn(field(obj, "cpu_millis"), "limits.cpu_millis", 1, uint32Max); err != nil {
		return err
	}
	return uintIn(field(obj, "blkio_weight"), "limits.blkio_weight", 10, 1000)
}

// validateCreateSpec: tam AppSpec.
func validateCreateSpec(spec *jsonv.Object) error {
	checks := []func() error{
		func() error { return checkSource(spec) },
		func() error { return checkDockerfile(field(spec, "dockerfile_path")) },
		func() error { return checkBuildArgs(field(spec, "build_args")) },
		func() error { _, err := checkEnv(field(spec, "env")); return err },
		func() error { _, err := checkVolumes(field(spec, "volumes")); return err },
		func() error { return uintIn(field(spec, "container_port"), "container_port", 1, 65535) },
		func() error { return uintIn(field(spec, "replicas"), "replicas", 1, maxReplicas) },
		func() error { return checkHealthPath(field(spec, "health_path")) },
		func() error { return checkDomain(field(spec, "domain"), "domain") },
		func() error { return checkLimits(field(spec, "limits")) },
	}
	for _, check := range checks {
		if err := check(); err != nil {
			return err
		}
	}
	return nil
}

// validateAppUpdate: yalnız verilen alanlar; sunucu birleşik tanımı ayrıca doğrular.
func validateAppUpdate(update *jsonv.Object) error {
	for _, key := range createOnlyKeys {
		if update.Has(key) {
			return errors.New("unknown app field")
		}
	}
	// Sunucu null'u "verilmedi" sayar, onay kutusu ise "boş değer" gösterirdi
	// (domain: null → "alan adı kaldırılıyor"). Anlam ayrılmasın diye reddedilir.
	for _, key := range updateScalars {
		if update.Has(key) && field(update, key) == nil {
			return bad("%s must not be null", key)
		}
	}
	if update.Has("domain") {
		if err := checkDomain(field(update, "domain"), "domain"); err != nil {
			return err
		}
	}
	if update.Has("git_branch") {
		if err := checkBranch(field(update, "git_branch")); err != nil {
			return err
		}
	}
	if update.Has("health_path") {
		if err := checkHealthPath(field(update, "health_path")); err != nil {
			return err
		}
	}
	if update.Has("replicas") {
		if err := uintIn(field(update, "replicas"), "replicas", 1, maxReplicas); err != nil {
			return err
		}
	}
	env, err := checkEnv(field(update, "env"))
	if err != nil {
		return err
	}
	volumes, err := checkVolumes(field(update, "volumes"))
	if err != nil {
		return err
	}
	if err := checkNameList(field(update, "env_remove"), "env_remove", buildArgName, env.Has); err != nil {
		return err
	}
	return checkNameList(field(update, "volume_remove"), "volume_remove", volumeName, func(name string) bool {
		return slices.Contains(volumes, name)
	})
}

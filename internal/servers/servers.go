// Package servers, iş istasyonunun kayıtlı sunucu listesidir (servers.json,
// K-162). CLI'ın `kadran server` ve `-s <ad>` yolları da masaüstünün yan
// süreci de bu paketi kullanır; dosyanın biçimini başka hiçbir yer bilmez.
//
// Dosyada anahtar, parola ya da jeton YOK: yalnız ad, hedef ve root'a hangi
// yoldan gidileceği. Ama dosya elle düzenlenebilir; okunan her hedef yazmadaki
// denetimden geçer, `-oProxyCommand=…` gibi bir değer ssh'a ulaşmaz.
package servers

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/erkanrzgc/kadran/internal/client"
)

// FileVersion, bu istemcinin yazdığı biçim. Daha yeni bir sürümün dosyası
// okunur ama yazılmaz: bilmediğimiz alanları silerek onu bozmayalım.
const FileVersion = 1

// Sınırlar masaüstünün bugünkü profil sınırlarıyla aynı.
const (
	MaxServers  = 100
	MaxName     = 64
	MaxProvider = 64
)

// Server, kayıtlı bir sunucudur. Target günlük iş (kadran-client), Admin
// kurulum ve yükseltme (root ya da Sudo ile parolasız sudo'lu kullanıcı).
type Server struct {
	Name     string `json:"name"`
	Provider string `json:"provider,omitempty"`
	Target   string `json:"target"`
	Admin    string `json:"admin,omitempty"`
	Sudo     bool   `json:"sudo,omitempty"`
}

var (
	ErrNotFound        = errors.New("no saved server")
	ErrNewerFile       = errors.New("the server list was written by a newer Kadran — upgrade this client to change it")
	ErrDuplicateName   = errors.New("a saved server already has this name")
	ErrDuplicateTarget = errors.New("a saved server already has this target")
)

// invisible, denetim ve görünmez biçim karakterleri (\p{Cf}: sıfır genişlikli
// boşluk, sağdan sola işaretleri, yumuşak tire). Cf'li iki ad ekranda aynı
// görünüp ayrı kayıt olurdu, yön işareti de adı ters gösterirdi.
func invisible(r rune) bool { return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) }

// NormalizeName, adı kırpar ve kurala uyduğunu denetler: 1–64 karakter,
// denetim ya da görünmez karakter yok, `-` ile başlamıyor (`-s -foo` bayrak
// gibi okunmasın).
func NormalizeName(name string) (string, error) {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return "", errors.New("server name is empty")
	case !utf8.ValidString(n):
		return "", errors.New("server name is not valid UTF-8")
	case utf8.RuneCountInString(n) > MaxName:
		return "", fmt.Errorf("server name is longer than %d characters", MaxName)
	case strings.HasPrefix(n, "-"):
		return "", fmt.Errorf("server name cannot start with `-` (%q)", n)
	case strings.IndexFunc(n, invisible) >= 0:
		return "", fmt.Errorf("server name contains a control or invisible character (%q)", n)
	}
	return n, nil
}

// validateEntry, tek bir kaydı denetler; hata kaydın adını taşır.
func validateEntry(s Server) error {
	n, err := NormalizeName(s.Name)
	if err != nil {
		return fmt.Errorf("server %q: %w", s.Name, err)
	}
	if n != s.Name {
		return fmt.Errorf("server %q: name has leading or trailing spaces", s.Name)
	}
	if s.Target == "" || strings.TrimSpace(s.Target) != s.Target {
		return fmt.Errorf("server %q: target is empty or has surrounding spaces", s.Name)
	}
	// Boş dize ParseTarget'ta yerel soket demek; yukarıda reddedildi.
	if _, err := client.ParseTarget(s.Target); err != nil {
		return fmt.Errorf("server %q: target: %w", s.Name, err)
	}
	if utf8.RuneCountInString(s.Provider) > MaxProvider || strings.IndexFunc(s.Provider, invisible) >= 0 {
		return fmt.Errorf("server %q: provider is longer than %d characters or has a control or invisible character", s.Name, MaxProvider)
	}
	if s.Admin == "" {
		if s.Sudo {
			return fmt.Errorf("server %q: sudo needs an admin target", s.Name)
		}
		return nil
	}
	admin, err := client.ParseTarget(s.Admin)
	if err != nil {
		return fmt.Errorf("server %q: admin: %w", s.Name, err)
	}
	if admin.IsLocal() || strings.TrimSpace(s.Admin) != s.Admin {
		return fmt.Errorf("server %q: admin must be an SSH target (root@server or user@server)", s.Name)
	}
	return nil
}

// Validate, bütün listeyi denetler: her kayıt, sayı sınırı ve benzersizlik.
// Ad benzersizliği büyük/küçük harfe bakmaz (Hetzner ile hetzner birlikte
// olamaz); arama ise tam eşleşmedir (Find).
func Validate(list []Server) error {
	if len(list) > MaxServers {
		return fmt.Errorf("at most %d servers can be saved", MaxServers)
	}
	for i, s := range list {
		if err := validateEntry(s); err != nil {
			return err
		}
		for _, other := range list[:i] {
			if strings.EqualFold(other.Name, s.Name) {
				return fmt.Errorf("%w: %q and %q", ErrDuplicateName, other.Name, s.Name)
			}
			if other.Target == s.Target {
				return fmt.Errorf("%w: %q (%s and %s)", ErrDuplicateTarget, s.Target, other.Name, s.Name)
			}
		}
	}
	return nil
}

// Find, adı TAM eşleşmeyle arar. Büyük/küçük harf, önek ya da benzerlik
// eşleşmesi yok: yanlış sunucuya dağıtım bu listenin en kötü hatası olur.
func Find(list []Server, name string) (Server, bool) {
	for _, s := range list {
		if s.Name == name {
			return s, true
		}
	}
	return Server{}, false
}

// Add, kaydı sona ekler. Ad ve sağlayıcı kırpılır (CLI de masaüstü de bu
// yoldan geçer); liste değişmez, yenisi döner.
func Add(list []Server, s Server) ([]Server, error) {
	n, err := NormalizeName(s.Name)
	if err != nil {
		return nil, err
	}
	s.Name = n
	s.Provider = strings.TrimSpace(s.Provider)
	next := append(slices.Clone(list), s)
	if err := Validate(next); err != nil {
		return nil, err
	}
	return next, nil
}

// Remove, adı tam eşleşen kaydı siler.
func Remove(list []Server, name string) ([]Server, error) {
	i := slices.IndexFunc(list, func(s Server) bool { return s.Name == name })
	if i < 0 {
		return nil, fmt.Errorf("%w named %q", ErrNotFound, name)
	}
	return slices.Delete(slices.Clone(list), i, i+1), nil
}

// Rename, kaydın adını değiştirir; hedef, yönetici ve sudo aynı kalır.
func Rename(list []Server, oldName, newName string) ([]Server, error) {
	i := slices.IndexFunc(list, func(s Server) bool { return s.Name == oldName })
	if i < 0 {
		return nil, fmt.Errorf("%w named %q", ErrNotFound, oldName)
	}
	n, err := NormalizeName(newName)
	if err != nil {
		return nil, err
	}
	next := slices.Clone(list)
	next[i].Name = n
	if err := Validate(next); err != nil {
		return nil, err
	}
	return next, nil
}

// UpdateByTarget, masaüstünün düzenleme penceresi: hedefle bulunan kaydın
// adını ve sağlayıcısını değiştirir (boş sağlayıcı siler). Yönetici hedefi
// ve sudo, CLI'ın yazdığı gibi kalır.
func UpdateByTarget(list []Server, target, name, provider string) ([]Server, error) {
	i := slices.IndexFunc(list, func(s Server) bool { return s.Target == target })
	if i < 0 {
		return nil, fmt.Errorf("%w with target %q", ErrNotFound, target)
	}
	n, err := NormalizeName(name)
	if err != nil {
		return nil, err
	}
	next := slices.Clone(list)
	next[i].Name = n
	next[i].Provider = strings.TrimSpace(provider)
	if err := Validate(next); err != nil {
		return nil, err
	}
	return next, nil
}

// RemoveByTarget, hedefle bulunan kaydı siler.
func RemoveByTarget(list []Server, target string) ([]Server, error) {
	i := slices.IndexFunc(list, func(s Server) bool { return s.Target == target })
	if i < 0 {
		return nil, fmt.Errorf("%w with target %q", ErrNotFound, target)
	}
	return slices.Delete(slices.Clone(list), i, i+1), nil
}

// Skipped, içe aktarmada alınamayan bir profil ve sebebi.
type Skipped struct {
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// Import, masaüstünün eski profil listesini bir kez taşır. Hedef kırpılır
// (eski masaüstü " kadran-client@h " ile de bağlanıyordu); listede olan hedef
// atlanır, bu kayıp değildir. Ad kurala uydurulur (baştaki boşluk ve `-`
// atılır, 64 karaktere kısaltılır; boş kalırsa hedefin sunucu adı) ve başka bir
// kayıtta varsa "ad (2)", "ad (3)" … olur. Alınamayan profil (geçersiz hedef,
// dolu liste) sebebiyle döner: göç bir kez koşar, kaybolan kart geri gelmez.
func Import(list []Server, in []Server) (out []Server, added int, skipped []Skipped) {
	out = slices.Clone(list)
	for _, p := range in {
		target := strings.TrimSpace(p.Target)
		if slices.ContainsFunc(out, func(s Server) bool { return s.Target == target }) {
			continue
		}
		if len(out) >= MaxServers {
			skipped = append(skipped, Skipped{Target: p.Target, Reason: fmt.Sprintf("the list is full (%d servers)", MaxServers)})
			continue
		}
		cand := Server{
			Name:     uniqueName(out, importName(p.Name, target)),
			Provider: truncateRunes(strings.TrimSpace(stripInvisible(p.Provider)), MaxProvider),
			Target:   target,
		}
		if err := validateEntry(cand); err != nil {
			skipped = append(skipped, Skipped{Target: p.Target, Reason: err.Error()})
			continue
		}
		out = append(out, cand)
		added++
	}
	return out, added, skipped
}

// importName, eski bir profil adını kurala uydurur. Baştaki boşluk ve `-`
// birlikte atılır ("-   -y" → "y"): kırpma sabit noktada biter.
func importName(name, target string) string {
	dashOrSpace := func(r rune) bool { return r == '-' || unicode.IsSpace(r) }
	n := strings.TrimLeftFunc(stripInvisible(name), dashOrSpace)
	if strings.TrimSpace(n) == "" {
		n = "server"
		if t, err := client.ParseTarget(target); err == nil && !t.IsLocal() {
			n = strings.TrimLeftFunc(t.SSHHost, dashOrSpace)
		}
	}
	return strings.TrimSpace(truncateRunes(strings.TrimSpace(n), MaxName))
}

// uniqueName, ad listede (büyük/küçük harfe bakmadan) varsa " (n)" ekler;
// ek 64 karaktere sığsın diye kök kısaltılır.
func uniqueName(list []Server, base string) string {
	taken := func(name string) bool {
		return slices.ContainsFunc(list, func(s Server) bool { return strings.EqualFold(s.Name, name) })
	}
	if !taken(base) {
		return base
	}
	for n := 2; ; n++ {
		suffix := fmt.Sprintf(" (%d)", n)
		cand := strings.TrimSpace(truncateRunes(base, MaxName-utf8.RuneCountInString(suffix))) + suffix
		if !taken(cand) {
			return cand
		}
	}
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

// stripInvisible, eski profil adından denetim ve görünmez karakterleri atar:
// profil göçte reddedilmesin, kuralı geçen adla gelsin.
func stripInvisible(s string) string {
	return strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, s)
}

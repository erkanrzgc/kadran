package connproto

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// ── Roller (K-131) ───────────────────────────────────────────────────
//
// Rol, anahtarın authorized_keys satırında, zorlanmış komutun argümanı
// olarak yazılıdır:
//
//	command="/usr/local/lib/kadran/kadran-connect -deploy=web,api",restrict ssh-ed25519 AAAA...
//
// Argüman yoksa anahtar yöneticidir; bugüne kadar kurulan her satır böyle
// ve değişmeden çalışmaya devam eder.
//
// # Rol neden kadrand'nin veritabanında değil?
//
// Anahtarı authorized_keys'e eklemek root ister (kadrand kadran-client'ın
// ev dizinine yazamaz, executor'ın bütçesinde yer yok). Anahtar zaten o
// satırda; rolü de oraya koymak, anahtarla yetkisini TEK satırda tutar.
// İkinci bir depo, ikisinin birbirinden kayması demekti: silinen anahtarın
// rolü kalır ya da eklenen anahtar rolsüz kalırdı.
//
// # Rol neden güvenilir?
//
// Zorlanmış komut, istemcinin istediği komutu YOK SAYAR; argümanlar
// istemcinin değil, authorized_keys'i yazanın (root) sözüdür. kadran-connect
// rolü önsöze yazar ve önsöz, istemcinin tek baytı okunmadan gönderilir.
// Parmak izinin güvenilirliği hangi varsayıma dayanıyorsa bu da ona dayanır.

// Roller.
const (
	// RoleAdmin her RPC'yi çağırabilir.
	RoleAdmin = "admin"

	// RoleDeploy yalnızca kapsamındaki uygulamaları dağıtabilir.
	RoleDeploy = "deploy"
)

// MaxDeployApps, bir dağıtım anahtarının kapsamındaki en fazla uygulama.
//
// Önsöz 4 KB ile sınırlı; 32 adet 32 karakterlik ad bunun çok altında
// kalıyor. Daha geniş bir kapsam, dağıtım anahtarını yöneticiye
// yaklaştırır ve muhtemelen bir hatadır.
const MaxDeployApps = 32

// ErrInvalidRole, rol ya da kapsam geçersiz olduğunda döner.
var ErrInvalidRole = errors.New("connproto: invalid role")

// appIDPattern, uygulama adının karakter kümesidir.
//
// internal/api/appvalidate.go'daki desenin KASITLI kopyası (orada
// gerekçesiyle: bu paket api'yi içe aktaramaz). Sapma yalnızca redde yol
// açar: api'nin kabul edip burada reddedilen ad kapsamda kullanılamaz,
// burada kabul edilip api'de reddedilen ad hiçbir uygulamaya karşılık
// gelmez. İkisinin aynı olduğunu api'deki bir test denetliyor.
//
// Bu desen aynı zamanda kabuk enjeksiyonu sınırıdır: sshd zorlanmış
// komutu kullanıcının kabuğuna `-c` ile verir.
var appIDPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// AppIDPattern, deseni drift testi için dışa açar.
func AppIDPattern() string { return appIDPattern.String() }

// ParseDeployScope, "web,api" biçimindeki kapsamı ayrıştırır.
//
// Boş kapsam, boş öğe, tekrar ve joker REDDEDİLİR: kapsamsız bir dağıtım
// anahtarı her uygulamayı dağıtabilirdi ve bu, yazım hatasıyla
// verilmemeli.
func ParseDeployScope(s string) ([]string, error) {
	if s == "" {
		return nil, fmt.Errorf("%w: empty deploy scope", ErrInvalidRole)
	}
	apps := strings.Split(s, ",")
	if err := (Identity{Role: RoleDeploy, Apps: apps}).CheckRole(); err != nil {
		return nil, err
	}
	return apps, nil
}

// CheckRole, rolün ve kapsamın tutarlı olduğunu doğrular.
//
// Bilinmeyen ya da BOŞ rol geçersizdir. Boş rolü yönetici saymak "eski
// kadran-connect"e uyum sağlardı ama bedeli, rolü yazmayı unutan her kod
// yolunun sessizce tam yetki vermesi olurdu.
func (id Identity) CheckRole() error {
	switch id.Role {
	case RoleAdmin:
		if len(id.Apps) != 0 {
			return fmt.Errorf("%w: the admin role carries no app scope", ErrInvalidRole)
		}
		return nil
	case RoleDeploy:
		return checkDeployApps(id.Apps)
	default:
		return fmt.Errorf("%w: %q", ErrInvalidRole, id.Role)
	}
}

func checkDeployApps(apps []string) error {
	if len(apps) == 0 {
		return fmt.Errorf("%w: the deploy role needs at least one app", ErrInvalidRole)
	}
	if len(apps) > MaxDeployApps {
		return fmt.Errorf("%w: the scope has %d apps, limit %d",
			ErrInvalidRole, len(apps), MaxDeployApps)
	}
	seen := make(map[string]bool, len(apps))
	for _, app := range apps {
		if !appIDPattern.MatchString(app) {
			return fmt.Errorf("%w: invalid app name %q", ErrInvalidRole, app)
		}
		if seen[app] {
			return fmt.Errorf("%w: %q appears twice in the scope", ErrInvalidRole, app)
		}
		seen[app] = true
	}
	return nil
}

// CanDeploy, kimliğin app'i dağıtıp dağıtamayacağını söyler.
//
// Geçersiz kimlik hiçbir şeyi dağıtamaz; el sıkışma onu zaten reddediyor,
// bu ikinci kat.
func (id Identity) CanDeploy(app string) bool {
	if id.CheckRole() != nil {
		return false
	}
	switch id.Role {
	case RoleAdmin:
		return true
	case RoleDeploy:
		return slices.Contains(id.Apps, app)
	default:
		return false
	}
}

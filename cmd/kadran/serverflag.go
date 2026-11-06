package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/erkanrzgc/kadran/internal/servers"
)

// ── `-s <ad>`: kayıtlı sunucuyu adıyla seçmek (K-162) ────────────────
//
// Çözüm run()'da bir kez yapılır: `-s ad` argümanlardan çıkarılır, kayıtlı
// hedef konumsal hedefin yerine (sona) eklenir. Her komut, oturum dosyası ve
// ipucu böylece sıradan bir hedef görür; komutların kendisi değişmez.
//
// Konumsal argümanlar komutun bayrakları bilinmeden ayırt edilemez. -s ile
// birlikte bir hedef de yazılırsa fazladan argümanı komutun kendi sayı
// denetimi reddeder; bunu her komut için TestServerFlagWithTargetNeverDials
// sınar. Yanlış sunucuya gitmek bu listenin en kötü hatası olur.

// serverFlagError, -s çözümünün hatası; usage, kullanım hatası mı (çıkış 2).
type serverFlagError struct {
	usage bool
	msg   string
}

func (e *serverFlagError) Error() string { return e.msg }

// noTargetCommands, hedef almayan komutlar: -s orada bir yanlışlıktır.
var noTargetCommands = map[string]bool{"version": true, "sidecar": true, "server": true}

// adminInsertAt, kök yolundan giden komutlarda -sudo'nun ekleneceği yer
// (komutun ve varsa alt komutun hemen ardı); öbür komutlarda -1.
func adminInsertAt(args []string) int {
	switch {
	case args[0] == "bootstrap":
		return 1
	case args[0] == "key" && len(args) > 1:
		return 2
	case args[0] == "totp" && len(args) > 1 && args[1] == "reset":
		return 2
	}
	return -1
}

// expandServerFlag, `-s ad`ı kayıtlı hedefle değiştirir. -s yoksa args aynen
// döner ve liste hiç okunmaz. Kök yolundan giden komutlara (bootstrap, key,
// totp reset) kaydın yönetici hedefi gider, sudo'luysa -sudo da eklenir;
// kullanıcının ayrıca yazdığı -sudo daha sonra geldiği için o geçerli olur.
func expandServerFlag(args []string, load func() ([]servers.Server, error)) ([]string, string, error) {
	if len(args) == 0 {
		return args, "", nil
	}
	name, rest, found, err := takeServerFlag(args)
	if err != nil || !found {
		return args, "", err
	}
	if noTargetCommands[args[0]] {
		return nil, "", &serverFlagError{usage: true, msg: fmt.Sprintf("`%s` does not take -s", args[0])}
	}
	list, err := load()
	if err != nil {
		return nil, "", &serverFlagError{msg: err.Error()}
	}
	srv, ok := servers.Find(list, name)
	if !ok {
		return nil, "", &serverFlagError{msg: fmt.Sprintf("%s named %q — %s", servers.ErrNotFound, name, savedNames(list))}
	}
	at := adminInsertAt(rest)
	if at < 0 {
		return append(slices.Clone(rest), srv.Target), name, nil
	}
	if srv.Admin == "" {
		return nil, "", &serverFlagError{msg: noAdminHint(srv, args[0])}
	}
	out := slices.Clone(rest[:at])
	if srv.Sudo {
		out = append(out, "-sudo")
	}
	out = append(out, rest[at:]...)
	return append(out, srv.Admin), name, nil
}

// takeServerFlag, args[1:]'de `--`a kadar -s, --s, -s=ad ve --s=ad'ı arar;
// bulursa adı ve -s'siz argümanları döner.
func takeServerFlag(args []string) (name string, rest []string, found bool, err error) {
	rest = []string{args[0]}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		var value string
		switch {
		case a == "-s" || a == "--s":
			if i+1 >= len(args) {
				return "", nil, false, &serverFlagError{usage: true, msg: "-s needs a saved server name"}
			}
			i++
			value = args[i]
		case strings.HasPrefix(a, "-s=") || strings.HasPrefix(a, "--s="):
			value = a[strings.Index(a, "=")+1:]
		default:
			rest = append(rest, a)
			continue
		}
		if found {
			return "", nil, false, &serverFlagError{usage: true, msg: "-s is given twice"}
		}
		if value == "" {
			return "", nil, false, &serverFlagError{usage: true, msg: "-s needs a saved server name"}
		}
		name, found = value, true
	}
	return name, rest, found, nil
}

// checkServerTarget, -s verildiyse komutun bağlanacağı hedefin kayıtlı hedef
// (kök yolunda yönetici hedefi) olduğunu denetler. Bir konumsal argüman eksik
// ya da fazlaysa kayıtlı hedef başka bir argümanın yerine kayar; o zaman
// hiçbir şey gönderilmez (K-162). Her komutun kendi sayı denetimine ek ikinci
// kilit: connect, bootstrap, key ve totp reset bunu çağırır.
func (c *cli) checkServerTarget(raw string) error {
	if c.serverName == "" || raw == c.serverTarget {
		return nil
	}
	shown, _ := shellName(c.serverName)
	return fmt.Errorf("-s %s: an argument is missing or extra (the saved target did not end up as the target, "+
		"so nothing was sent) — see the command's usage", shown)
}

// noAdminHint, yönetici hedefi olmayan kayıtla kök yolundan bir komut
// denenince ne yapılacağı. Komutlar ayrı satırlarda (PowerShell 5.1'de && yok)
// ve sağlayıcı korunarak; ad kabukta güvenle yazılamıyorsa komut verilmez.
func noAdminHint(srv servers.Server, cmd string) string {
	shown, ok := shellName(srv.Name)
	head := fmt.Sprintf("server %s has no admin target for `%s` — save it again with one", shown, cmd)
	if !ok {
		return head + " (kadran server rm, then kadran server add -admin root@host)"
	}
	add := "kadran server add"
	if srv.Provider != "" {
		if p, pok := shellName(srv.Provider); pok {
			add += " -provider " + p
		}
	}
	return head + ":\n" +
		"  kadran server rm " + shown + "\n" +
		"  " + add + " -admin root@host " + shown + " " + srv.Target + "\n" +
		"  (or -admin user@host -sudo for a user with passwordless sudo)"
}

// shellName, kayıtlı adın ipuçlarındaki biçimi; ok, kopyalanıp bash,
// PowerShell ya da cmd'de çalıştırılabilir mi. Harfle başlayan ve yalnız
// harf, rakam, `.`, `_`, `-` taşıyan ad olduğu gibi yazılır. Boşluk, parantez
// gibi üç kabuğun da çift tırnak içinde düz okuduğu karakterler varsa çift
// tırnaklanır ("a (2)"). Tırnak içinde de yorumlanan bir karakter ($ ` " \ !
// % ya da listede olmayan bir işaret) varsa ok=false: ad yalnız gösterilir,
// çalıştırılacak bir komuta yazılmaz.
func shellName(name string) (string, bool) {
	plain, quotable := name != "", true
	for i, r := range name {
		switch {
		case unicode.IsLetter(r):
		case unicode.IsDigit(r) || unicode.IsMark(r) || strings.ContainsRune("._-", r):
			plain = plain && i > 0
		case strings.ContainsRune(" ()+=/,:@'#&;[]~", r):
			plain = false
		default:
			plain, quotable = false, false
		}
	}
	switch {
	case plain:
		return name, true
	case quotable:
		return `"` + name + `"`, true
	}
	return strconv.Quote(name), false
}

// savedNames, "kayıt yok" hatasının ikinci yarısı: kayıtlı adlar ya da boş
// listenin nasıl doldurulacağı. Benzer ad önerilmez.
func savedNames(list []servers.Server) string {
	if len(list) == 0 {
		return "the list is empty; add one: kadran server add <name> <target>"
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	return "saved: " + strings.Join(names, ", ")
}

package main

import (
	"fmt"
	"slices"
	"strings"

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
		return nil, "", &serverFlagError{msg: fmt.Sprintf(
			"server %q has no admin target for `%s` — save it again with one: "+
				"kadran server rm %s && kadran server add -admin root@host %s %s",
			name, args[0], name, name, srv.Target)}
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

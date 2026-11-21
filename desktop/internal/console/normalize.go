package console

import (
	"errors"
	"strings"

	"github.com/erkanrzgc/kadran/internal/cli"
)

// Sunucu bağlamı hataları.
var (
	ErrOtherServer = errors.New("the console runs on the selected server only: switch servers with `use <name>` or remove -s")
	ErrTarget      = errors.New("the console targets the selected server only: ssh targets and socket paths are not accepted")
)

// Normalize, ayrıştırılmış sözcükleri seçili sunucuya bağlar: baştaki tek
// "kadran" atılır; hedef alan bir komutta kullanıcının yazdığı -s/--s seçili
// sunucuya eşitse çıkarılır (başka sunucuysa ErrOtherServer) ve sona
// -s <server> eklenir. Hedef almayan ya da bilinmeyen komuta dokunulmaz; satırı
// çalıştırıcı Inspect ile reddeder. Girdi dilimi değiştirilmez.
func Normalize(words []string, server string) ([]string, error) {
	if len(words) > 0 && words[0] == "kadran" {
		words = words[1:]
	}
	if len(words) == 0 {
		return nil, nil
	}
	if server == "" || !takesTarget(words[0]) {
		return append([]string(nil), words...), nil
	}
	rest, requested, err := stripServerFlags(words)
	if err != nil {
		return nil, err
	}
	for _, r := range requested {
		if r != server {
			return nil, ErrOtherServer
		}
	}
	return append(rest, "-s", server), nil
}

// stripServerFlags, "-s X", "-s=X", "--s X" ve "--s=X" biçimlerini sözcüklerden
// ayırır; bulunan değerleri sırayla döner. Değersiz -s ErrOtherServer sayılır.
func stripServerFlags(words []string) (rest, requested []string, err error) {
	rest = make([]string, 0, len(words)+2)
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "-s" || w == "--s":
			if i+1 >= len(words) {
				return nil, nil, ErrOtherServer
			}
			i++
			requested = append(requested, words[i])
		case strings.HasPrefix(w, "-s="):
			requested = append(requested, strings.TrimPrefix(w, "-s="))
		case strings.HasPrefix(w, "--s="):
			requested = append(requested, strings.TrimPrefix(w, "--s="))
		default:
			rest = append(rest, w)
		}
	}
	return rest, requested, nil
}

// takesTarget, ilk sözcüğün hedef alan bilinen bir üst düzey komut olduğunu
// söyler; bilinmeyen komutta false döner.
func takesTarget(first string) bool {
	for _, c := range cli.Commands() {
		if c.Name == first {
			return !c.NoTarget
		}
	}
	return false
}

// RejectTarget, konumsal bir argüman ssh hedefi (@) ya da soket yolu (/ ile
// başlayan) gibi görünüyorsa ErrTarget döner: konsol yalnız seçili sunucuyla
// konuşur.
func RejectTarget(inv cli.Invocation) error {
	for _, p := range inv.Positional {
		if strings.Contains(p, "@") || strings.HasPrefix(p, "/") {
			return ErrTarget
		}
	}
	return nil
}

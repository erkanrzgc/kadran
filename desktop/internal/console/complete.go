package console

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// ── Tab ile tamamlama (spec Tasarım 7) ───────────────────────────────

const (
	// appsTTL, uygulama adlarının önbellekte en çok bayat kalma süresi.
	appsTTL = 30 * time.Second
	// appsTimeout, tamamlama için yapılan uygulama listesi çağrısının sınırı.
	appsTimeout = 5 * time.Second
)

// appTakers, ilk konumsal argümanı bir uygulama adı olan komutlar.
var appTakers = map[string]bool{
	"deploy": true, "rollback": true, "logs": true, "prune": true,
	"app show": true, "app update": true, "app delete": true,
}

type appsEntry struct {
	names []string
	at    time.Time
}

type appsCache struct {
	mu      sync.Mutex
	entries map[string]appsEntry
}

// Complete, imlecin bulunduğu sözcük için adayları döner (tam sözcükler,
// sıralı). cursor, JavaScript'in verdiği UTF-16 birimidir. Komut ve alt komut
// adları, bayraklar (Commands()'tan), uygulama adları (Apps, 30 sn önbellek)
// ve `use` için kayıtlı sunucular önerilir; yasak komutlar önerilmez.
func (r *Runner) Complete(server, line string, cursor int) []string {
	prefix := line[:byteOffset(line, cursor)]
	words := strings.Fields(prefix)
	current := ""
	if len(words) > 0 && !strings.HasSuffix(prefix, " ") {
		current, words = words[len(words)-1], words[:len(words)-1]
	}
	if len(words) > 0 && words[0] == "kadran" {
		words = words[1:]
	}
	return withPrefix(r.candidates(server, words, current), current)
}

func (r *Runner) candidates(server string, words []string, current string) []string {
	if len(words) == 0 {
		return topLevelNames()
	}
	if words[0] == "use" {
		if len(words) == 1 {
			return r.serverNames()
		}
		return nil
	}
	node, ok := findCommand(cli.Commands(), words[0])
	if !ok {
		return nil
	}
	path, rest := []string{node.Name}, words[1:]
	if len(node.Subs) > 0 {
		if len(rest) == 0 {
			return allowedNames(node.Subs)
		}
		if node, ok = findCommand(node.Subs, rest[0]); !ok {
			return nil
		}
		path, rest = append(path, node.Name), rest[1:]
	}
	if strings.HasPrefix(current, "-") {
		return flagNames(node, current)
	}
	if expectsValue(node, rest) || !appTakers[strings.Join(path, " ")] || hasPositional(node, rest) {
		return nil
	}
	return r.appNames(server)
}

// topLevelNames, üst düzey komutlar (yasaklar hariç) ve konsolun yerleşikleri.
func topLevelNames() []string {
	return append(allowedNames(cli.Commands()), "use", "clear")
}

// allowedNames, yasak olmayan düğümlerin adları. Alt komutlu bir düğüm,
// en az bir yaprağı yasak değilse önerilir.
func allowedNames(nodes []cli.Command) []string {
	var out []string
	for _, n := range nodes {
		if n.Class == cli.ClassBanned {
			continue
		}
		if len(n.Subs) > 0 && len(allowedNames(n.Subs)) == 0 {
			continue
		}
		out = append(out, n.Name)
	}
	return out
}

func flagNames(node cli.Command, current string) []string {
	dash := "-"
	if strings.HasPrefix(current, "--") {
		dash = "--"
	}
	out := make([]string, 0, len(node.Flags))
	for _, f := range node.Flags {
		out = append(out, dash+f.Name)
	}
	return out
}

// expectsValue: son sözcük değer bekleyen (bool olmayan, "="siz) bir bayrak.
func expectsValue(node cli.Command, rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	f, ok := flagOf(node, rest[len(rest)-1])
	return ok && !f.Bool
}

// hasPositional, Go'nun flag paketi gibi okur: ilk bayrak olmayan sözcükte
// konumsallar başlar (bool olmayan bayrağın değeri atlanır).
func hasPositional(node cli.Command, rest []string) bool {
	for i := 0; i < len(rest); i++ {
		w := rest[i]
		if !strings.HasPrefix(w, "-") || w == "-" || w == "--" {
			return w != "--" || i+1 < len(rest)
		}
		if f, ok := flagOf(node, w); ok && !f.Bool {
			i++
		}
	}
	return false
}

// flagOf, "=" içermeyen "-ad"/"--ad" sözcüğünün bayrağını bulur.
func flagOf(node cli.Command, word string) (cli.Flag, bool) {
	if !strings.HasPrefix(word, "-") || strings.Contains(word, "=") {
		return cli.Flag{}, false
	}
	name := strings.TrimPrefix(strings.TrimPrefix(word, "-"), "-")
	for _, f := range node.Flags {
		if f.Name == name {
			return f, true
		}
	}
	return cli.Flag{}, false
}

func (r *Runner) serverNames() []string {
	if r.d.Servers == nil {
		return nil
	}
	list, err := r.d.Servers()
	if err != nil {
		return nil
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = s.Name
	}
	return names
}

// appNames, kayıtlı bir sunucunun uygulama adları; en çok appsTTL bayat.
// Hata önbelleğe alınmaz. Kayıtta olmayan ad için Apps hiç çağrılmaz.
func (r *Runner) appNames(server string) []string {
	if server == "" || r.d.Apps == nil || !slices.Contains(r.serverNames(), server) {
		return nil
	}
	now := r.d.Now()
	r.apps.mu.Lock()
	entry, ok := r.apps.entries[server]
	r.apps.mu.Unlock()
	if ok && now.Sub(entry.at) < appsTTL {
		return entry.names
	}
	ctx, cancel := context.WithTimeout(context.Background(), appsTimeout)
	defer cancel()
	names, err := r.d.Apps(ctx, server)
	if err != nil {
		return nil
	}
	names = slices.Clone(names)
	r.apps.mu.Lock()
	if len(r.apps.entries) >= servers.MaxServers {
		r.apps.entries = map[string]appsEntry{}
	}
	r.apps.entries[server] = appsEntry{names: names, at: now}
	r.apps.mu.Unlock()
	return names
}

// withPrefix, önekle başlayan adayları sıralı ve tekil döner (asla nil).
func withPrefix(cands []string, prefix string) []string {
	out := []string{}
	for _, c := range cands {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// byteOffset, UTF-16 birimiyle verilen imleci bayt konumuna çevirir; satır
// dışındaki imleç kenara çekilir.
func byteOffset(line string, cursor int) int {
	if cursor <= 0 {
		return 0
	}
	units := 0
	for i, r := range line {
		if units >= cursor {
			return i
		}
		units += utf16.RuneLen(r)
	}
	return len(line)
}

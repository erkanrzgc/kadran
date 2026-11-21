package console

import (
	"errors"
	"fmt"
	"strings"

	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// ── Konsola özgü komutlar: use, clear, help (spec Tasarım 4) ─────────

// consoleHelp, CLI'ın komut listesinden sonra yazılan konsol kısayolları.
const consoleHelp = "\nConsole commands:\n" +
	"  use <server>   switch the selected server (commands run there; -s is added for you)\n" +
	"  clear          clear the output\n" +
	"  help           this list\n" +
	"\nKeys:\n" +
	"  Enter          run the line\n" +
	"  ↑/↓            history\n" +
	"  Tab            complete\n" +
	"  Ctrl+C         stop the running command\n" +
	"  Ctrl+L         clear the output\n" +
	"  Ctrl+`         open or close the console\n"

// slashHelp, help için eğik çizgili kısayoldur; eğik çizgiyle başlayan başka
// hiçbir sözcük yerleşik sayılmaz.
const slashHelp = "/help"

// builtin, yerleşik komutu çalıştırır; ok false ise satır yerleşik değildir.
// Tek başına -h ve --help de help'tir (R7: `deploy -h` ise CLI'a gider).
func (r *Runner) builtin(rs *runState, list []servers.Server, words []string) (int, bool) {
	switch {
	case words[0] == "use":
		return r.use(rs, list, words[1:]), true
	case words[0] == "clear":
		if len(words) > 1 {
			return rs.refuse(lineErr(errors.New("usage: clear"))), true
		}
		rs.out.event(EventClear, ClearData{})
		return cli.ExitOK, true
	case len(words) == 1 && (words[0] == "help" || words[0] == slashHelp || words[0] == "-h" || words[0] == "--help"):
		return r.help(rs), true
	}
	return 0, false
}

// use, seçili sunucuyu değiştirir (kenar çubuğu console:use ile izler).
// Çalışma sürerken Run zaten ErrBusy döndüğü için buraya ulaşmaz.
func (r *Runner) use(rs *runState, list []servers.Server, args []string) int {
	if len(args) != 1 {
		return rs.refuse(lineErr(fmt.Errorf("usage: use <server> (saved: %s)", savedNames(list))))
	}
	srv, ok := servers.Find(list, args[0])
	if !ok {
		return rs.refuse(fmt.Errorf("no saved server named %q (saved: %s)", args[0], savedNames(list)))
	}
	rs.out.event(EventUse, UseData{Server: srv.Name})
	return cli.ExitOK
}

func savedNames(list []servers.Server) string {
	if len(list) == 0 {
		return "none; add one in the app"
	}
	names := make([]string, len(list))
	for i, s := range list {
		names[i] = quote(s.Name)
	}
	return strings.Join(names, ", ")
}

// help, CLI'ın komut listesini (okuma sınıfıyla) basar, ardından konsol
// kısayollarını yazar.
func (r *Runner) help(rs *runState) int {
	rio := r.newRunIO(rs, false)
	exit := r.d.Run(rs.ctx, []string{"help"}, rio.env)
	fmt.Fprint(rio.env.Stdout, consoleHelp)
	return exit
}

// findLeaf, argümanların başındaki komut (ve alt komut) adından yaprağı
// bulur; bayraklara bakmaz.
func findLeaf(args []string) ([]string, cli.Command, bool) {
	nodes := cli.Commands()
	var path []string
	for _, w := range args {
		node, ok := findCommand(nodes, w)
		if !ok {
			return nil, cli.Command{}, false
		}
		path = append(path, node.Name)
		if len(node.Subs) == 0 {
			return path, node, true
		}
		nodes = node.Subs
	}
	return nil, cli.Command{}, false
}

// leafWords, satırın komut yolu kısmı (hata ipucu için).
func leafWords(args []string) []string {
	if path, _, ok := findLeaf(args); ok {
		return path
	}
	if len(args) > 0 {
		return args[:1]
	}
	return nil
}

func findCommand(nodes []cli.Command, name string) (cli.Command, bool) {
	for _, n := range nodes {
		if n.Name == name {
			return n, true
		}
	}
	return cli.Command{}, false
}

package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ── Komut ağacı, sınıflar ve Inspect (K-169) ─────────────────────────
//
// Masaüstü konsolu bir satırı ÇALIŞTIRMADAN önce hangi komut olduğunu,
// hangi sınıfa girdiğini ve hangi bayrakların verildiğini bilmek zorunda.
// Bunun için komutun kendi FlagSet kurucusu kullanılır: Commands()'taki
// bayrak listesi de, Inspect'in ayrıştırması da, komutun çalışırken
// kullandığı kurucudan türer. Elle yazılmış ikinci bir bayrak listesi yok;
// komutun bayrağı değişince burası kendiliğinden değişir.

// Class, bir komutun konsolda nasıl karşılanacağını belirler.
type Class string

// Komut sınıfları (spec Tasarım 2).
const (
	ClassRead    Class = "read"
	ClassSession Class = "session"
	ClassChange  Class = "change"
	ClassDanger  Class = "danger"
	ClassRoot    Class = "root"
	ClassBanned  Class = "banned"
)

// ErrUnclassified: satır ağaçtaki bir yaprağa çözülemedi (bilinmeyen komut,
// eksik ya da bilinmeyen alt komut). Konsol bunu reddeder (kapalı varsayılan).
var ErrUnclassified = errors.New("command is not classified")

// Flag, bir komutun bayrağı.
type Flag struct {
	Name  string
	Usage string
	Bool  bool
}

// Command, komut ağacının bir düğümü. Sınıf yaprakta durur; alt komutu olan
// komutun Class'ı boştur. Bir bayrağın değeri sınıfı değiştirebiliyorsa
// (prune -dry-run, audit verify -anchors) Class en katı olandır ve gerçek
// sınıf için Inspect kullanılır.
type Command struct {
	Name, Args, Summary string
	Class               Class
	// NoTarget: komut sunucu hedefi almaz; konsol ona -s eklemez.
	NoTarget bool
	// Flags, yaprağın FlagSet kurucusundan türetilir (ad sırasıyla).
	Flags []Flag
	Subs  []Command

	// flags yaprağın FlagSet kurucusu; run üst düzey komutun çalıştırıcısı
	// (nil: run()'ın kendi işlediği yapay komut, ör. help); refine, bayrak
	// değerlerine göre sınıfı ayarlar (nil: Class aynen).
	flags  func(c *cli) *flag.FlagSet
	run    func(c *cli, ctx context.Context, args []string) int
	refine func(flags map[string][]string) Class
}

// Invocation, bir satırın çalıştırılmadan çözülmüş hâli.
type Invocation struct {
	// Path, yaprağın yolu: ["app", "create"].
	Path  []string
	Class Class
	// Flags yalnız satırda verilen bayrakları taşır; değerler verildiği
	// sırayla, tekrarlar korunarak.
	Flags      map[string][]string
	Positional []string
}

// fsOnly, (FlagSet, değerler) döndüren bir kurucudan yalnız FlagSet'i alır.
func fsOnly[T any](b func(*cli) (*flag.FlagSet, T)) func(*cli) *flag.FlagSet {
	return func(c *cli) *flag.FlagSet {
		fs, _ := b(c)
		return fs
	}
}

// plainFlags, bayrağı olmayan bir komutun (boş) bayrak kümesi.
func plainFlags(name string) func(*cli) *flag.FlagSet {
	return func(c *cli) *flag.FlagSet { return c.newFlagSet(name) }
}

// tree, komut ağacını (bayraklar türetilmeden) kurar. Bir İŞLEV: newFlagSet
// isNoTarget üzerinden buna bakıyor, paket değişkeni olsaydı başlatma
// döngüsü olurdu.
func tree() []Command {
	return []Command{
		{Name: "status", Args: "[target]", Summary: "shows server and daemon status",
			Class: ClassRead, run: (*cli).runStatus, flags: fsOnly((*cli).statusFlags)},
		{Name: "app", Args: "<create|update|list|show|delete> …", Summary: "manages app definitions",
			run: (*cli).runApp, Subs: []Command{
				{Name: "create", Class: ClassChange, flags: fsOnly((*cli).appCreateFlags)},
				{Name: "update", Class: ClassChange, flags: fsOnly((*cli).appUpdateFlagSet)},
				{Name: "list", Class: ClassRead, flags: fsOnly((*cli).appListFlags)},
				{Name: "show", Class: ClassRead, flags: fsOnly((*cli).appShowFlags)},
				{Name: "delete", Class: ClassDanger, flags: fsOnly((*cli).appDeleteFlags)},
			}},
		{Name: "deploy", Args: "<app> [target]", Summary: "builds a commit and switches traffic to it",
			Class: ClassChange, run: (*cli).runDeploy, flags: fsOnly((*cli).deployFlags)},
		{Name: "rollback", Args: "<app> [target]", Summary: "switches traffic back to the previous release",
			Class: ClassDanger, run: (*cli).runRollback, flags: fsOnly((*cli).rollbackFlags)},
		{Name: "logs", Args: "[-f] <app> [target]", Summary: "streams the live release's output",
			Class: ClassRead, run: (*cli).runLogs, flags: fsOnly((*cli).logsFlags)},
		{Name: "prune", Args: "[-dry-run] <app>|-all [target]", Summary: "removes old releases' containers",
			Class: ClassDanger, run: (*cli).runPrune, flags: fsOnly((*cli).pruneFlags), refine: refinePrune},
		{Name: "alarms", Args: "[target]", Summary: "lists active fault conditions",
			Class: ClassRead, run: (*cli).runAlarms, flags: fsOnly((*cli).alarmsFlags)},
		{Name: "backup", Args: "<create|list> [target]", Summary: "takes and lists database backups",
			run: (*cli).runBackup, Subs: []Command{
				{Name: "create", Class: ClassChange, flags: fsOnly((*cli).backupCreateFlags)},
				{Name: "list", Class: ClassRead, flags: fsOnly((*cli).backupListFlags)},
			}},
		{Name: "domain", Args: "check <domain> [target]", Summary: "checks a domain's DNS, ports and certificate",
			run: (*cli).runDomain, Subs: []Command{
				{Name: "check", Class: ClassRead, flags: fsOnly((*cli).domainCheckFlags)},
			}},
		{Name: "audit", Args: "<list|verify> [target]", Summary: "reads and verifies the audit chain",
			run: (*cli).runAudit, Subs: []Command{
				{Name: "list", Class: ClassRead, flags: fsOnly((*cli).auditListFlags)},
				{Name: "verify", Class: ClassRead, flags: fsOnly((*cli).auditVerifyFlags), refine: refineAuditVerify},
			}},
		{Name: "server", Args: "<add|list|rm|rename> …", Summary: "manages the saved server list",
			NoTarget: true, run: (*cli).runServer, Subs: []Command{
				{Name: "add", Class: ClassBanned, flags: fsOnly((*cli).serverAddFlags)},
				{Name: "list", Class: ClassRead, flags: plainFlags("server list")},
				{Name: "rm", Class: ClassBanned, flags: plainFlags("server rm")},
				{Name: "rename", Class: ClassBanned, flags: plainFlags("server rename")},
			}},
		{Name: "sidecar", Summary: "stdio JSON-RPC server for the desktop app",
			Class: ClassBanned, NoTarget: true, run: (*cli).runSidecar, flags: fsOnly((*cli).sidecarFlags)},
		{Name: "bootstrap", Args: "root@server | -sudo user@server", Summary: "installs or upgrades the server",
			Class: ClassBanned, run: (*cli).runBootstrap, flags: fsOnly((*cli).bootstrapFlags)},
		{Name: "github", Args: "<connect|status|disconnect> [target]", Summary: "connects private GitHub repositories",
			run: (*cli).runGitHub, Subs: []Command{
				{Name: "connect", Class: ClassChange, flags: githubLeaf("github connect")},
				{Name: "status", Class: ClassRead, flags: githubLeaf("github status")},
				{Name: "disconnect", Class: ClassDanger, flags: githubLeaf("github disconnect")},
			}},
		{Name: "key", Args: "<list|add|remove> … root@server", Summary: "manages deploy-only keys",
			run: (*cli).runKey, Subs: []Command{
				{Name: "list", Class: ClassRoot, flags: fsOnly((*cli).keyListFlags)},
				{Name: "add", Class: ClassRoot, flags: fsOnly((*cli).keyAddFlags)},
				{Name: "remove", Class: ClassRoot, flags: fsOnly((*cli).keyRemoveFlags)},
			}},
		{Name: "login", Args: "[-recovery] [target]", Summary: "signs in with a TOTP code (when TOTP is on)",
			Class: ClassSession, run: (*cli).runLogin, flags: fsOnly((*cli).loginFlags)},
		{Name: "logout", Args: "[target]", Summary: "deletes the saved sign-in for a server",
			Class: ClassSession, run: (*cli).runLogout, flags: plainFlags("logout")},
		{Name: "totp", Args: "<status|enable|disable|reset> [target]", Summary: "manages two-step sign-in",
			run: (*cli).runTotp, Subs: []Command{
				{Name: "status", Class: ClassRead, flags: plainFlags("totp status")},
				{Name: "enable", Class: ClassDanger, flags: totpProofLeaf("totp enable")},
				{Name: "disable", Class: ClassDanger, flags: totpProofLeaf("totp disable")},
				{Name: "reset", Class: ClassRoot, flags: fsOnly((*cli).totpResetFlags)},
			}},
		{Name: "version", Summary: "prints version information",
			Class: ClassRead, NoTarget: true, run: (*cli).runVersion, flags: plainFlags("version")},
		// help, run()'ın kendi işlediği yapay yaprak: kullanım listesinde ve
		// dağıtımda yok (run nil), konsol için okuma sınıfında.
		{Name: "help", Class: ClassRead, NoTarget: true, flags: plainFlags("help")},
	}
}

func githubLeaf(name string) func(*cli) *flag.FlagSet {
	return func(c *cli) *flag.FlagSet { return c.githubFlags(name) }
}

func totpProofLeaf(name string) func(*cli) *flag.FlagSet {
	return func(c *cli) *flag.FlagSet {
		fs, _ := c.totpProofFlags(name)
		return fs
	}
}

// refinePrune: yalnız `-dry-run` AÇIKKEN (`-dry-run=false` değil) değiştirme
// sınıfı; yoksa her şey, `-all` dahil, tehlikeli.
func refinePrune(flags map[string][]string) Class {
	if lastBool(flags["dry-run"]) {
		return ClassChange
	}
	return ClassDanger
}

// refineAuditVerify: `-anchors` verilmişse (boş değerle bile, katı yana
// yanılır) iş istasyonundan dosya okur: yasak.
func refineAuditVerify(flags map[string][]string) Class {
	if _, given := flags["anchors"]; given {
		return ClassBanned
	}
	return ClassRead
}

func lastBool(values []string) bool {
	if len(values) == 0 {
		return false
	}
	on, err := strconv.ParseBool(values[len(values)-1])
	return err == nil && on
}

// isNoTarget, üst düzey komutun hedef almadığını söyler (-s orada hata).
func isNoTarget(name string) bool {
	for _, cmd := range tree() {
		if cmd.Name == name {
			return cmd.NoTarget
		}
	}
	return false
}

// newInspectCLI, bayrak kurucularının istediği cli: hiçbir şey yazmaz,
// bağlanmaz.
func newInspectCLI() *cli {
	return &cli{stdin: eofReader{}, stdout: io.Discard, stderr: io.Discard}
}

type eofReader struct{}

func (eofReader) Read([]byte) (int, error) { return 0, io.EOF }

// Commands, komut ağacını bayraklarıyla döndürür. Bayraklar yaprağın
// FlagSet kurucusundan okunur (VisitAll).
func Commands() []Command {
	c := newInspectCLI()
	return withFlags(c, tree())
}

func withFlags(c *cli, cmds []Command) []Command {
	for i := range cmds {
		if cmds[i].flags != nil {
			cmds[i].Flags = flagsOf(cmds[i].flags(c))
		}
		cmds[i].Subs = withFlags(c, cmds[i].Subs)
	}
	return cmds
}

func flagsOf(fs *flag.FlagSet) []Flag {
	var out []Flag
	fs.VisitAll(func(f *flag.Flag) {
		out = append(out, Flag{Name: f.Name, Usage: f.Usage, Bool: isBoolFlag(f.Value)})
	})
	return out
}

func isBoolFlag(v flag.Value) bool {
	b, ok := v.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}

// commands, kullanım metninin ve dağıtımın tablosu: ad, args ve summary
// Commands()'ın dayandığı ağaçtan gelir (tek yer).
func commands() []command {
	var out []command
	for _, n := range tree() {
		if n.run != nil {
			out = append(out, command{name: n.Name, args: n.Args, summary: n.Summary, run: n.run})
		}
	}
	return out
}

// recorder, bir bayrağın her Set çağrısını sırayla kaydeder; asıl değere
// aynen iletir. Bool bayrağı olma özelliğini korur.
type recorder struct {
	inner flag.Value
	name  string
	seen  map[string][]string
}

func (r *recorder) String() string {
	if r == nil || r.inner == nil {
		return ""
	}
	return r.inner.String()
}

func (r *recorder) Set(v string) error {
	if err := r.inner.Set(v); err != nil {
		return err
	}
	r.seen[r.name] = append(r.seen[r.name], v)
	return nil
}

func (r *recorder) IsBoolFlag() bool { return isBoolFlag(r.inner) }

// Inspect, bir satırın argümanlarını (komut adıyla başlar, `-s` içermez)
// ÇALIŞTIRMADAN çözer: yaprağı, bayrakları, konumsal argümanları ve sınıfı.
// Ayrıştırma komutun kendi FlagSet kurucusuyla yapılır, hiçbir yere yazmaz,
// bağlanmaz.
func Inspect(args []string) (Invocation, error) {
	leaf, path, rest, err := resolveLeaf(args)
	if err != nil {
		return Invocation{}, err
	}
	fs := leaf.flags(newInspectCLI())
	fs.SetOutput(io.Discard)
	seen := map[string][]string{}
	fs.VisitAll(func(f *flag.Flag) {
		f.Value = &recorder{inner: f.Value, name: f.Name, seen: seen}
	})
	if err := fs.Parse(rest); err != nil {
		return Invocation{}, err
	}
	class := leaf.Class
	if leaf.refine != nil {
		class = leaf.refine(seen)
	}
	return Invocation{Path: path, Class: class, Flags: seen, Positional: fs.Args()}, nil
}

// resolveLeaf, args'ın başındaki komut (ve alt komut) adından yaprağı bulur.
func resolveLeaf(args []string) (leaf Command, path, rest []string, err error) {
	if len(args) == 0 {
		return Command{}, nil, nil, fmt.Errorf("%w: empty line", ErrUnclassified)
	}
	nodes, path, rest := tree(), []string(nil), args
	for {
		node, ok := findNode(nodes, rest[0])
		if !ok {
			return Command{}, nil, nil, fmt.Errorf("%w: %q", ErrUnclassified, strings.Join(append(path, rest[0]), " "))
		}
		path, rest = append(path, node.Name), rest[1:]
		if len(node.Subs) == 0 {
			return node, path, rest, nil
		}
		if len(rest) == 0 {
			return Command{}, nil, nil, fmt.Errorf("%w: %q needs a subcommand", ErrUnclassified, strings.Join(path, " "))
		}
		nodes = node.Subs
	}
}

func findNode(nodes []Command, name string) (Command, bool) {
	for _, n := range nodes {
		if n.Name == name {
			return n, true
		}
	}
	return Command{}, false
}

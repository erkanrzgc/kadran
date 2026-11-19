package console

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// ── Satırın kapısı ───────────────────────────────────────────────────
//
// Sıra (her adım yalnız reddedebilir; cli.Run'a giden tek yol sonuncusu):
//   ParseLine → (geçmiş) → yerleşikler (use, clear, help) → Normalize →
//   `--` reddi → Inspect → yasak → RejectTarget → gizli geçmiş değeri (R9)
//   → sınıf kapısı (sunucu, root ayarı, yönetici hedefi) → yazılı ad
//   (tehlikeli, root) → onay kutusu (okuma ve oturum dışı her sınıf) →
//   iptal ve root ayarı yeniden → cli.Run.
// Yasak denetimi RejectTarget'tan öne alındı: `bootstrap root@x` hedef
// hatası yerine "CLI'da çalıştır" demeli; ikisi de ret, sıra yol açmaz.
//
// Sınıf, çalışacak olanla ayrışamaz: Inspect ve CLI aynı FlagSet kurucusuyla
// aynı argümanları ayrıştırır; tek fark, CLI'ın sona eklediği hedeftir (-s
// çözümü) ve bayrak ayrıştırması ilk konumsalda durduğu için o hedef hiçbir
// bayrağın değerine dönüşemez.

// effectAdminKey, yönetici ayarı kutusunun etki anahtarı.
const effectAdminKey = "console admin"

// Ret nedenleri (stderr'e yazılır).
var (
	ErrNoServer     = errors.New("select a server with use <name> first (only help, use and server list work without one)")
	ErrHiddenValues = errors.New("this line has hidden values from history; type them again")
	ErrMismatch     = errors.New("confirmation did not match: nothing was run")
	ErrDeclined     = errors.New("cancelled: nothing was run")
	ErrCancelled    = errors.New("stopped before running: nothing was run")
	ErrDashDash     = errors.New("-- is not supported in the console")
)

// refusal, çıkış kodunu taşıyan ret.
type refusal struct {
	exit int
	err  error
}

func (f *refusal) Error() string { return f.err.Error() }
func (f *refusal) Unwrap() error { return f.err }

// lineErr, satırın kendisindeki hata: çıkış kodu kullanım hatası (2).
func lineErr(err error) error { return &refusal{exit: cli.ExitUsage, err: err} }

// plan, onaylanmış ya da onaylanacak bir komut.
type plan struct {
	server servers.Server
	inv    cli.Invocation
	full   []string // cli.Run'a giden argümanlar (eklenen -s dahil)
	line   string   // arıtılmış satır, eklenen -s olmadan
	typed  string   // yazılı onayda beklenen ad; "" yazılı onay yok
	admin  string   // kutudaki yönetici hedefi (yalnız root)
}

func (p plan) root() bool { return p.inv.Class == cli.ClassRoot }

// pipeline, satırı uçtan uca işler ve çıkış kodunu döner.
func (r *Runner) pipeline(rs *runState, server, line string) int {
	words, err := ParseLine(line)
	if err != nil {
		return rs.refuse(lineErr(err))
	}
	if len(words) == 0 {
		return cli.ExitOK
	}
	r.remember(rs, server, Redact(words))
	if words[0] == "kadran" {
		words = words[1:]
	}
	if len(words) == 0 {
		return cli.ExitOK
	}
	list := r.savedServers(rs)
	srv, _ := servers.Find(list, server)
	if exit, ok := r.builtin(rs, list, words); ok {
		return exit
	}
	p, err := r.prepare(srv, words)
	if err != nil {
		return rs.refuse(err)
	}
	if err := r.approve(rs, p); err != nil {
		return rs.refuse(err)
	}
	return r.launch(rs, p)
}

// remember, satırı (yazıldığı gibi, arıtılmış) geçmişe ekler.
func (r *Runner) remember(rs *runState, server, redacted string) {
	if err := r.d.History.Append(server, redacted); err != nil {
		fmt.Fprintf(rs.out.writer(StreamErr), "console history could not be saved: %v\n", err)
	}
}

// savedServers, kayıtlı sunucuları okur; okunamazsa uyarır ve boş döner
// (sunucu seçilmemiş sayılır: kapalı yön).
func (r *Runner) savedServers(rs *runState) []servers.Server {
	if r.d.Servers == nil {
		return nil
	}
	list, err := r.d.Servers()
	if err != nil {
		fmt.Fprintf(rs.out.writer(StreamErr), "saved servers could not be read: %v\n", err)
		return nil
	}
	return list
}

// prepare, satırı çözer ve sınıf kapısından geçirir. srv.Name boşsa sunucu
// seçilmemiştir (arayüzün verdiği ad kayıtta yoksa da böyle sayılır).
func (r *Runner) prepare(srv servers.Server, words []string) (plan, error) {
	full, err := Normalize(words, srv.Name)
	if err != nil {
		return plan{}, lineErr(err)
	}
	// CLI -s'yi `--`dan sonra aramaz: eklenen -s orada konumsal kalır ve
	// Inspect'in gördüğüyle CLI'ın çözdüğü ayrışırdı.
	if slices.Contains(full, "--") {
		return plan{}, lineErr(ErrDashDash)
	}
	bare := withoutInjected(full, srv.Name)
	inv, err := inspect(bare)
	if err != nil {
		return plan{}, err
	}
	if err := checkInvocation(inv); err != nil {
		return plan{}, err
	}
	p := plan{server: srv, inv: inv, full: full, line: Redact(bare)}
	if err := r.gate(&p); err != nil {
		return plan{}, err
	}
	return p, nil
}

// withoutInjected, Normalize'ın sona eklediği "-s <sunucu>" çiftini atar:
// Inspect -s'siz argüman ister, kutu ve kayıt da sunucuyu ayrıca gösterir.
func withoutInjected(full []string, server string) []string {
	if server == "" || len(full) < 2 || !takesTarget(full[0]) {
		return full
	}
	return full[:len(full)-2]
}

// inspect, cli.Inspect'i çağırır. Yardım bayrağı (`deploy -h`) okuma
// sayılır (R7): CLI yalnız yardımı basar. Yasak yaprağın yardımı da yasak.
func inspect(bare []string) (cli.Invocation, error) {
	inv, err := cli.Inspect(bare)
	if err == nil {
		return inv, nil
	}
	if !errors.Is(err, flag.ErrHelp) {
		if errors.Is(err, cli.ErrUnclassified) {
			return cli.Invocation{}, lineErr(fmt.Errorf("%w (type help for the commands)", err))
		}
		return cli.Invocation{}, lineErr(fmt.Errorf("%w (type %s -h for its flags)", err, strings.Join(leafWords(bare), " ")))
	}
	path, leaf, ok := findLeaf(bare)
	if !ok {
		return cli.Invocation{}, lineErr(err)
	}
	class := cli.ClassRead
	if leaf.Class == cli.ClassBanned {
		class = cli.ClassBanned
	}
	return cli.Invocation{Path: path, Class: class}, nil
}

// checkInvocation: yasak komut, ssh hedefi/soket yolu ve geçmişten gelen
// gizli değer reddedilir.
func checkInvocation(inv cli.Invocation) error {
	if inv.Class == cli.ClassBanned {
		return bannedReason(strings.Join(inv.Path, " "))
	}
	if err := RejectTarget(inv); err != nil {
		return lineErr(err)
	}
	if hasMaskedEnv(inv.Flags["env"]) {
		return lineErr(ErrHiddenValues)
	}
	return nil
}

// bannedReason, yasak komutun nedenini söyler (spec Tasarım 2).
func bannedReason(path string) error {
	switch path {
	case "bootstrap":
		return errors.New("bootstrap is not available in the console: run it in the CLI")
	case "audit verify":
		return errors.New("audit verify -anchors reads files on this computer: run it in the CLI")
	case "server add", "server rm", "server rename":
		return errors.New("saved servers are added, removed and renamed in the app, not in the console")
	}
	return fmt.Errorf("%s is not available in the console", path)
}

// hasMaskedEnv (R9): geçmişten geri çağrılan satırda -env değeri "***"
// olarak durur; çalışsaydı sır gerçekten "***" olurdu.
func hasMaskedEnv(values []string) bool {
	for _, v := range values {
		if v == redacted {
			return true
		}
		if _, val, ok := strings.Cut(v, "="); ok && val == redacted {
			return true
		}
	}
	return false
}

// gate, sınıfa göre izin verir ve yazılı onayın beklenen adını ve kutudaki
// yönetici hedefini doldurur. Bilinmeyen sınıf reddedilir (kapalı varsayılan).
func (r *Runner) gate(p *plan) error {
	switch p.inv.Class {
	case cli.ClassRead, cli.ClassSession, cli.ClassChange, cli.ClassDanger, cli.ClassRoot:
	default:
		return fmt.Errorf("%w: %s", cli.ErrUnclassified, strings.Join(p.inv.Path, " "))
	}
	if p.server.Name == "" && !worksWithoutServer(p.inv.Path) {
		return ErrNoServer
	}
	// Kutu satırı kesmeden gösterir (R10); sığmayan satır yazılı ad ve kutudan
	// önce reddedilir. Çıkış 1, öteki politika retleri gibi.
	if needsBox(p.inv.Class) && confirm.ConsoleLineUnits(p.line) > confirm.MaxConsoleLineUnits {
		return errors.New(confirm.ConsoleTooLong(r.language()))
	}
	switch p.inv.Class {
	case cli.ClassDanger:
		p.typed = dangerName(p.inv, p.server.Name)
	case cli.ClassRoot:
		if !r.d.Admin.Load() {
			return ErrAdminOff
		}
		if p.server.Admin == "" {
			return fmt.Errorf("server %s has no admin target: save it again with one in the app", p.server.Name)
		}
		p.admin = adminShown(p.server, p.inv)
		p.typed = p.server.Name
	}
	if (p.inv.Class == cli.ClassDanger || p.root()) && p.typed == "" {
		return lineErr(fmt.Errorf("%s needs a name to confirm (type %s -h for usage)", strings.Join(p.inv.Path, " "), strings.Join(p.inv.Path, " ")))
	}
	return nil
}

func worksWithoutServer(path []string) bool {
	joined := strings.Join(path, " ")
	return joined == "help" || joined == "server list"
}

// dangerName: app delete, rollback ve prune <app> için uygulama adı;
// prune -all, github disconnect ve totp enable/disable için sunucu adı.
func dangerName(inv cli.Invocation, server string) string {
	switch strings.Join(inv.Path, " ") {
	case "app delete", "rollback":
		return firstOf(inv.Positional)
	case "prune":
		if flagOn(inv.Flags["all"]) {
			return server
		}
		return firstOf(inv.Positional)
	}
	return server
}

// adminShown, kutuda gösterilen yönetici hedefi. CLI kayıttaki Sudo'yu da
// satırdaki -sudo'yu da uygular; kutu ikisini de göstermeli.
func adminShown(srv servers.Server, inv cli.Invocation) string {
	if srv.Sudo || flagOn(inv.Flags["sudo"]) {
		return "-sudo " + srv.Admin
	}
	return srv.Admin
}

// flagOn, bool bayrağın son değeri (Go'nun flag paketi gibi son kazanır).
func flagOn(values []string) bool {
	if len(values) == 0 {
		return false
	}
	on, err := strconv.ParseBool(values[len(values)-1])
	return err == nil && on
}

func firstOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// effectKey, komut yolunun etki anahtarı; prune'ın dry-run kipi ayrı.
func effectKey(inv cli.Invocation) string {
	key := strings.Join(inv.Path, " ")
	if key == "prune" && inv.Class == cli.ClassChange {
		return "prune -dry-run"
	}
	return key
}

// needsBox: okuma ve oturum DIŞINDAKİ her sınıf kutudan geçer; bilinmeyen
// bir sınıf da (kapı zaten reddeder) kutusuz geçemez.
func needsBox(class cli.Class) bool {
	return class != cli.ClassRead && class != cli.ClassSession
}

// approve, yazılı adı ve yerel onay kutusunu ister. Yalnız (true, nil)
// onaydır; sonrasında iptal ve (root'ta) ayar yeniden denetlenir.
func (r *Runner) approve(rs *runState, p plan) error {
	if p.typed != "" {
		if err := r.typedName(rs, p.typed); err != nil {
			return err
		}
	}
	if needsBox(p.inv.Class) {
		if err := r.askBox(p); err != nil {
			return err
		}
	}
	if rs.ctx.Err() != nil {
		return ErrCancelled
	}
	if p.root() && !r.d.Admin.Load() {
		return ErrAdminOff
	}
	return nil
}

// typedName, "type <ad> to confirm: " istemiyle bir satır ister ve ad ile
// TAM (büyük/küçük harf duyarlı, kırpmasız) karşılaştırır. Yazılan metin
// hiçbir yere yazılmaz.
func (r *Runner) typedName(rs *runState, want string) error {
	fmt.Fprintf(rs.out.writer(StreamOut), "type %s to confirm: ", want)
	got, err := rs.in.readText()
	if err != nil {
		return ErrCancelled
	}
	if got != want {
		return ErrMismatch
	}
	return nil
}

func (r *Runner) askBox(p plan) error {
	lang := r.language()
	effect, ok := confirm.ConsoleEffect(lang, effectKey(p.inv))
	if !ok {
		return fmt.Errorf("no confirmation text for %s: not run", strings.Join(p.inv.Path, " "))
	}
	box := confirm.BuildConsole(lang, confirm.ConsoleRequest{Server: p.server.Name, Line: p.line, Effect: effect, Admin: p.admin})
	approved, err := r.confirm(box)
	if err != nil {
		return fmt.Errorf("confirmation failed: %w", err)
	}
	if !approved {
		return ErrDeclined
	}
	return nil
}

// refuse, nedeni stderr'e yazar ve çıkış kodunu döner.
func (rs *runState) refuse(err error) int {
	fmt.Fprintln(rs.out.writer(StreamErr), err)
	var f *refusal
	if errors.As(err, &f) {
		return f.exit
	}
	return cli.ExitError
}

package cli

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// leafEntry, komut ağacının bir yaprağı: tam yol ve komut.
type leafEntry struct {
	path []string
	cmd  Command
}

func leavesOf(prefix []string, cmds []Command) []leafEntry {
	var out []leafEntry
	for _, cmd := range cmds {
		path := append(append([]string{}, prefix...), cmd.Name)
		if len(cmd.Subs) == 0 {
			out = append(out, leafEntry{path, cmd})
			continue
		}
		out = append(out, leavesOf(path, cmd.Subs)...)
	}
	return out
}

func joinPath(p []string) string { return strings.Join(p, " ") }

// dispatchedLeaves, dağıtıcıların (commands() adları ve her run…'ın alt
// komut switch'i) kabul ettiği yaprak yolları: ağaçtan BAĞIMSIZ, elle
// tutulan liste. Yeni bir komut ya da alt komut eklenince burası da
// güncellenmek zorunda; ağaç unutulursa test düşer.
var dispatchedLeaves = []string{
	"status", "app create", "app update", "app list", "app show", "app delete",
	"deploy", "rollback", "logs", "prune", "alarms",
	"backup create", "backup list", "domain check",
	"audit list", "audit verify",
	"server add", "server list", "server rm", "server rename",
	"sidecar", "bootstrap",
	"github connect", "github status", "github disconnect",
	"key list", "key add", "key remove",
	"login", "logout",
	"totp status", "totp enable", "totp disable", "totp reset",
	"version", "help",
}

func TestEveryLeafClassified(t *testing.T) {
	var got []string
	for _, l := range leavesOf(nil, Commands()) {
		got = append(got, joinPath(l.path))
		if l.cmd.Class == "" {
			t.Errorf("%q yaprağının sınıfı yok", joinPath(l.path))
		}
	}
	want := append([]string{}, dispatchedLeaves...)
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ağacın yaprakları dağıtıcılarla aynı değil\n ağaç : %v\n kabul: %v", got, want)
	}

	// Alt komutu olan komutun kendi sınıfı olmaz: sınıf yaprakta.
	for _, cmd := range Commands() {
		if len(cmd.Subs) > 0 && cmd.Class != "" {
			t.Errorf("%q alt komutlu ama kendi sınıfı var (%q)", cmd.Name, cmd.Class)
		}
	}
}

// TestCommandsMatchUsageTable, commands() (kullanım ve dağıtım) ile
// Commands()'ın aynı üst düzey komutları taşıdığını doğrular: ad, args ve
// summary tek yerde duruyor.
func TestCommandsMatchUsageTable(t *testing.T) {
	var fromTree []string
	for _, cmd := range Commands() {
		if cmd.Name == "help" {
			continue // run()'ın kendi işlediği yapay yaprak
		}
		fromTree = append(fromTree, cmd.Name+"|"+cmd.Args+"|"+cmd.Summary)
	}
	var fromTable []string
	for _, cmd := range commands() {
		fromTable = append(fromTable, cmd.name+"|"+cmd.args+"|"+cmd.summary)
	}
	if !reflect.DeepEqual(fromTree, fromTable) {
		t.Errorf("commands() Commands()'tan türemiyor\n ağaç : %v\n tablo: %v", fromTree, fromTable)
	}
}

func TestNoTargetFlags(t *testing.T) {
	want := map[string]bool{"version": true, "sidecar": true, "server": true, "help": true}
	for _, cmd := range Commands() {
		if cmd.NoTarget != want[cmd.Name] {
			t.Errorf("%q NoTarget = %v, beklenen %v", cmd.Name, cmd.NoTarget, want[cmd.Name])
		}
	}
}

func TestClassTable(t *testing.T) {
	tests := []struct {
		args []string
		want Class
	}{
		{[]string{"status"}, ClassRead},
		{[]string{"app", "list"}, ClassRead},
		{[]string{"app", "show", "web"}, ClassRead},
		{[]string{"logs", "web"}, ClassRead},
		{[]string{"alarms"}, ClassRead},
		{[]string{"backup", "list"}, ClassRead},
		{[]string{"domain", "check", "a.dev"}, ClassRead},
		{[]string{"audit", "list"}, ClassRead},
		{[]string{"audit", "verify"}, ClassRead},
		{[]string{"github", "status"}, ClassRead},
		{[]string{"totp", "status"}, ClassRead},
		{[]string{"server", "list"}, ClassRead},
		{[]string{"version"}, ClassRead},
		{[]string{"help"}, ClassRead},

		{[]string{"login"}, ClassSession},
		{[]string{"logout"}, ClassSession},

		{[]string{"app", "create", "-repo", "github.com/a/b", "web"}, ClassChange},
		{[]string{"app", "update", "-replicas", "2", "web"}, ClassChange},
		{[]string{"deploy", "web"}, ClassChange},
		{[]string{"backup", "create"}, ClassChange},
		{[]string{"prune", "-dry-run", "web"}, ClassChange},
		{[]string{"github", "connect"}, ClassChange},

		{[]string{"app", "delete", "web"}, ClassDanger},
		{[]string{"rollback", "web"}, ClassDanger},
		{[]string{"prune", "web"}, ClassDanger},
		{[]string{"prune", "-all"}, ClassDanger},
		{[]string{"github", "disconnect"}, ClassDanger},
		{[]string{"totp", "enable"}, ClassDanger},
		{[]string{"totp", "disable"}, ClassDanger},

		{[]string{"key", "list", "root@h"}, ClassRoot},
		{[]string{"key", "add", "-deploy", "a", "k.pub", "root@h"}, ClassRoot},
		{[]string{"key", "remove", "SHA256:x", "root@h"}, ClassRoot},
		{[]string{"totp", "reset", "root@h"}, ClassRoot},

		{[]string{"sidecar"}, ClassBanned},
		{[]string{"server", "add", "a", "b"}, ClassBanned},
		{[]string{"server", "rm", "a"}, ClassBanned},
		{[]string{"server", "rename", "a", "b"}, ClassBanned},
		{[]string{"bootstrap", "root@h"}, ClassBanned},
		{[]string{"audit", "verify", "-anchors", "f"}, ClassBanned},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			inv, err := Inspect(tt.args)
			if err != nil {
				t.Fatalf("Inspect: %v", err)
			}
			if inv.Class != tt.want {
				t.Errorf("sınıf = %q, beklenen %q", inv.Class, tt.want)
			}
		})
	}
}

// TestPruneDryRunFalseIsNotDryRun, `-dry-run=false`ın tehlikeli kalmasını
// sınar: bayrağın VARLIĞI değil, değeri sınıfı belirler.
func TestPruneDryRunFalseIsNotDryRun(t *testing.T) {
	inv, err := Inspect([]string{"prune", "-dry-run=false", "web"})
	if err != nil {
		t.Fatal(err)
	}
	if inv.Class != ClassDanger {
		t.Errorf("-dry-run=false sınıfı = %q, beklenen %q", inv.Class, ClassDanger)
	}
}

// goldenHelp, bir yaprağın `-h` altın dosyası (scripts/cli-golden.sh'ın
// vakaları): yaprak yolu → dosya numarası.
var goldenHelp = map[string]string{
	"status":       "06",
	"app create":   "08",
	"deploy":       "09",
	"logs":         "10",
	"prune":        "11",
	"audit verify": "12",
	"login":        "19",
}

var helpFlagLine = regexp.MustCompile(`(?m)^  -([A-Za-z][A-Za-z0-9-]*)`)

func flagNames(flags []Flag) []string {
	names := make([]string, 0, len(flags))
	for _, f := range flags {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return names
}

func TestInspectUsesRunFlags(t *testing.T) {
	for _, l := range leavesOf(nil, Commands()) {
		path := joinPath(l.path)
		t.Run(path, func(t *testing.T) {
			if l.cmd.flags == nil {
				t.Fatal("yaprağın FlagSet kurucusu yok")
			}
			if _, err := Inspect(append(append([]string{}, l.path...), "-yok")); err == nil {
				t.Error("bilinmeyen bayrak (-yok) reddedilmedi")
			}
			for _, f := range l.cmd.Flags {
				if !acceptsFlag(l.path, f) {
					t.Errorf("Inspect, Commands()'taki -%s bayrağını kabul etmedi", f.Name)
				}
			}
		})
	}
}

// acceptsFlag, bayrağı kendi türüne uyan bir değerle verip Inspect'in
// kabul ettiğini ve kaydettiğini denetler.
func acceptsFlag(path []string, f Flag) bool {
	values := []string{"1", "1s", "A=1", "vol:/data", "x"}
	if f.Bool {
		values = []string{""}
	}
	for _, v := range values {
		arg := "-" + f.Name
		if !f.Bool {
			arg += "=" + v
		}
		inv, err := Inspect(append(append([]string{}, path...), arg))
		if err == nil && len(inv.Flags[f.Name]) == 1 {
			return true
		}
	}
	return false
}

// TestFlagsMatchGoldenHelp, golden `-h` çıktısı olan yapraklarda
// Commands()'taki bayrakların komutun gerçekten yazdırdığı bayraklarla
// aynı olduğunu doğrular (`-s ad` satırını newFlagSet ekler, bayrak değil).
func TestFlagsMatchGoldenHelp(t *testing.T) {
	byPath := map[string]Command{}
	for _, l := range leavesOf(nil, Commands()) {
		byPath[joinPath(l.path)] = l.cmd
	}
	for path, id := range goldenHelp {
		t.Run(path, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "scripts", "testdata", "cli-golden", id+".err"))
			if err != nil {
				t.Fatal(err)
			}
			text := string(raw)
			if !strings.HasPrefix(text, "Usage of "+path+":") {
				t.Fatalf("%s.err %q için değil: %.40q", id, path, text)
			}
			var want []string
			for _, m := range helpFlagLine.FindAllStringSubmatch(text, -1) {
				if m[1] != "s" {
					want = append(want, m[1])
				}
			}
			sort.Strings(want)
			cmd, ok := byPath[path]
			if !ok {
				t.Fatalf("ağaçta %q yok", path)
			}
			if got := flagNames(cmd.Flags); !reflect.DeepEqual(got, want) {
				t.Errorf("Commands() bayrakları = %v, -h çıktısı = %v", got, want)
			}
		})
	}
}

func TestInspectKeepsRepeatedEnv(t *testing.T) {
	inv, err := Inspect([]string{"app", "create", "-env", "A=1", "-env=B=2", "web"})
	if err != nil {
		t.Fatal(err)
	}
	if got := inv.Flags["env"]; !reflect.DeepEqual(got, []string{"A=1", "B=2"}) {
		t.Errorf("Flags[env] = %v", got)
	}
	if !reflect.DeepEqual(inv.Positional, []string{"web"}) {
		t.Errorf("Positional = %v", inv.Positional)
	}
	if !reflect.DeepEqual(inv.Path, []string{"app", "create"}) {
		t.Errorf("Path = %v", inv.Path)
	}
	if _, given := inv.Flags["json"]; given {
		t.Error("verilmeyen -json Flags'te görünüyor")
	}
}

func TestInspectBoolFlagIsRecorded(t *testing.T) {
	inv, err := Inspect([]string{"status", "-json", "hedef"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(inv.Flags["json"], []string{"true"}) {
		t.Errorf("Flags[json] = %v", inv.Flags["json"])
	}
	if !reflect.DeepEqual(inv.Positional, []string{"hedef"}) {
		t.Errorf("Positional = %v", inv.Positional)
	}
}

func TestUnknownCommand(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"yok"},
		{"-h"},
		{"app"},                // alt komut eksik
		{"app", "yok"},         // bilinmeyen alt komut
		{"app", "-json"},       // alt komut yerine bayrak
		{"totp"},               // alt komut eksik
		{"server", "yok", "x"}, // bilinmeyen alt komut
	} {
		_, err := Inspect(args)
		if !errors.Is(err, ErrUnclassified) {
			t.Errorf("Inspect(%v) = %v, beklenen ErrUnclassified", args, err)
		}
	}
}

// TestInspectHelpFlagDoesNotPanic, `-h`'nin FlagSet'in yardım yolunu
// (PrintDefaults) çalıştırdığını ve sessiz kaldığını sınar.
func TestInspectHelpFlagDoesNotPanic(t *testing.T) {
	for _, l := range leavesOf(nil, Commands()) {
		if _, err := Inspect(append(append([]string{}, l.path...), "-h")); err == nil {
			t.Errorf("%q -h hata döndürmedi", joinPath(l.path))
		}
	}
}

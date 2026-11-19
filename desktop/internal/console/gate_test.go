package console

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/cli"
)

func TestDangerNeedsTypedName(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)

	id := h.start("hetzner", "app delete web")
	if in := h.waitInput(id); in.Prompt != "type web to confirm: " {
		t.Fatalf("istem %q", in.Prompt)
	}
	if err := h.r.Input(id, "weB"); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit == cli.ExitOK {
		t.Fatal("eşleşmeyen ad kabul edildi")
	}
	if h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatal("eşleşmeyen addan sonra kutu ya da Run")
	}
	if !strings.Contains(h.output(id, StreamErr), "confirmation did not match") {
		t.Fatalf("stderr %q", h.output(id, StreamErr))
	}
	if strings.Contains(h.output(id, ""), "weB") {
		t.Fatal("yazılan ad çıktıya girdi")
	}

	id = h.start("hetzner", "app delete web")
	h.waitInput(id)
	if err := h.r.Input(id, "web"); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit != cli.ExitOK || h.callCount() != 1 || h.boxCount() != 1 {
		t.Fatalf("exit %d, calls %d, boxes %d", exit, h.callCount(), h.boxCount())
	}

	for line, want := range map[string]string{"prune -all": "hetzner", "totp disable": "hetzner", "rollback api": "api", "prune old": "old"} {
		id := h.start("hetzner", line)
		if in := h.waitInput(id); in.Prompt != "type "+want+" to confirm: " {
			t.Fatalf("%s: istem %q", line, in.Prompt)
		}
		if err := h.r.Input(id, want); err != nil {
			t.Fatal(err)
		}
		if exit := h.done(id); exit != cli.ExitOK {
			t.Fatalf("%s: exit %d", line, exit)
		}
	}
}

func TestPruneDryRunIsChange(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	_, exit := h.exec("hetzner", "prune -dry-run web")
	if exit != cli.ExitOK || h.callCount() != 1 {
		t.Fatalf("exit %d", exit)
	}
	for _, e := range h.snapshot() {
		if e.Name == EventInput {
			t.Fatal("dry-run yazılı onay istedi")
		}
	}
	if !strings.Contains(h.lastBox().Detail, "Removes nothing") {
		t.Fatalf("dry-run etkisi: %q", h.lastBox().Detail)
	}
	id := h.start("hetzner", "prune -dry-run=false web")
	h.waitInput(id)
	if err := h.r.Cancel(id); err != nil {
		t.Fatal(err)
	}
	h.done(id)
}

func TestCancelWhileTypingName(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	id := h.start("hetzner", "app delete web")
	h.waitInput(id)
	if err := h.r.Cancel(id); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit == cli.ExitOK || h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatalf("iptal edilen onay çalıştı: exit %d", exit)
	}
}

func TestRootGate(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	id, exit := h.exec("hetzner", "key list")
	if exit == cli.ExitOK || h.callCount() != 0 {
		t.Fatalf("ayar kapalıyken root çalıştı: exit %d", exit)
	}
	if !strings.Contains(h.output(id, StreamErr), ErrAdminOff.Error()) {
		t.Fatalf("stderr %q", h.output(id, StreamErr))
	}

	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	id = h.start("hetzner", "key list")
	if in := h.waitInput(id); in.Prompt != "type hetzner to confirm: " {
		t.Fatalf("istem %q", in.Prompt)
	}
	if err := h.r.Input(id, "hetzner"); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit != cli.ExitOK {
		t.Fatalf("exit %d", exit)
	}
	if got := h.lastCall().args; !slices.Equal(got, []string{"key", "list", "-s", "hetzner"}) {
		t.Fatalf("args %q", got)
	}
	if d := h.lastBox().Detail; !strings.Contains(d, "root@1.2.3.4") {
		t.Fatalf("kutuda yönetici hedefi yok: %q", d)
	}
	raw, err := os.ReadFile(filepath.Join(h.dir, adminLogFile))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], `"command":"key list"`) || !strings.Contains(lines[0], `"server":"hetzner"`) {
		t.Fatalf("yönetici kaydı %q", raw)
	}
}

func TestRootBoxShowsSudoAdmin(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	for server, want := range map[string]string{"gcp": "-sudo ops@5.6.7.8", "hetzner": "-sudo root@1.2.3.4"} {
		line := "totp reset"
		if server == "hetzner" {
			line = "totp reset -sudo"
		}
		id := h.start(server, line)
		h.waitInput(id)
		if err := h.r.Input(id, server); err != nil {
			t.Fatal(err)
		}
		h.done(id)
		if d := h.lastBox().Detail; !strings.Contains(d, "Admin target: "+want) {
			t.Fatalf("%s: %q", server, d)
		}
	}
}

func TestRootNeedsAdminTarget(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	id, exit := h.exec("bare", "key list")
	if exit == cli.ExitOK || h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatalf("yönetici hedefi olmadan root: exit %d", exit)
	}
	if !strings.Contains(h.output(id, StreamErr), "no admin target") {
		t.Fatalf("stderr %q", h.output(id, StreamErr))
	}
}

func TestAdminRecheckedAfterConfirm(t *testing.T) {
	h := newHarness(t)
	setting := AdminSetting{Dir: h.dir}
	if err := setting.Save(true); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	h.answerFn = func(confirm.Box) (bool, error) { return true, setting.Save(false) }
	h.mu.Unlock()
	id := h.start("hetzner", "key list")
	h.waitInput(id)
	if err := h.r.Input(id, "hetzner"); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit == cli.ExitOK || h.callCount() != 0 {
		t.Fatalf("kutu açıkken kapatılan ayar yine de çalıştırdı: exit %d", exit)
	}
}

func TestBannedRefused(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	cases := map[string]string{
		"bootstrap root@x":        "run it in the CLI",
		"audit verify -anchors f": "run it in the CLI",
		"server add":              "not in the console",
		"server rm x":             "not in the console",
		"sidecar":                 "not available in the console",
		"bootstrap -h":            "run it in the CLI",
	}
	for line, want := range cases {
		id, exit := h.exec("hetzner", line)
		if exit == cli.ExitOK {
			t.Errorf("%s: exit 0", line)
		}
		if got := h.output(id, StreamErr); !strings.Contains(got, want) {
			t.Errorf("%s: stderr %q, want %q", line, got, want)
		}
	}
	if h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatalf("yasak komut ilerledi: calls %d boxes %d", h.callCount(), h.boxCount())
	}
}

func TestUnclassifiedRefused(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	for _, line := range []string{"frobnicate", "app", "app frob web", "deploy -nope web"} {
		id, exit := h.exec("hetzner", line)
		if exit == cli.ExitOK || h.output(id, StreamErr) == "" {
			t.Errorf("%s: exit %d stderr %q", line, exit, h.output(id, StreamErr))
		}
	}
	if h.callCount() != 0 {
		t.Fatal("sınıfsız satır çalıştı")
	}
}

func TestLineErrorsRefused(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	for _, line := range []string{"deploy web | sh", "deploy web -s gcp", "status root@evil", `deploy "web`, "deploy -- web", "app delete -- web"} {
		id, exit := h.exec("hetzner", line)
		if exit == cli.ExitOK || h.output(id, StreamErr) == "" {
			t.Errorf("%s: exit %d stderr %q", line, exit, h.output(id, StreamErr))
		}
	}
	if h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatal("hatalı satır ilerledi")
	}
}

func TestSetAdminNeedsConfirm(t *testing.T) {
	h := newHarness(t)
	setting := AdminSetting{Dir: h.dir}
	h.setAnswer(false)
	if on, err := h.r.SetAdmin(true); err != nil || on || setting.Load() {
		t.Fatalf("reddedilen kutu ayarı açtı: on %v err %v", on, err)
	}
	h.setAnswer(true)
	if on, err := h.r.SetAdmin(true); err != nil || !on || !setting.Load() {
		t.Fatalf("onaylanan kutu ayarı açmadı: on %v err %v", on, err)
	}
	if !strings.Contains(boxText(h.lastBox()), "admin") {
		t.Fatalf("kutu metni %q", boxText(h.lastBox()))
	}
	boxes := h.boxCount()
	if on, err := h.r.SetAdmin(false); err != nil || on || setting.Load() {
		t.Fatalf("kapatma: on %v err %v", on, err)
	}
	if h.boxCount() != boxes {
		t.Fatal("kapatmak onay istememeli")
	}
	if h.r.State("hetzner").Admin != setting.Load() {
		t.Fatal("State.Admin ayarı yansıtmıyor")
	}
}

func TestRootAddsStrictHostKey(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var got [][]string
	h.setRun(func(ctx context.Context, _ []string, env cli.Env) int {
		cmd := env.Command(ctx, "ssh", "-T", "root@1.2.3.4", "true")
		mu.Lock()
		got = append(got, slices.Clone(cmd.Args))
		mu.Unlock()
		return cli.ExitOK
	})
	id := h.start("hetzner", "key list")
	h.waitInput(id)
	if err := h.r.Input(id, "hetzner"); err != nil {
		t.Fatal(err)
	}
	h.done(id)
	h.exec("hetzner", "status")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("%d kanca çağrısı", len(got))
	}
	root, read := got[0], got[1]
	at := slices.Index(root, "StrictHostKeyChecking=yes")
	if at < 2 || root[at-1] != "-o" || at > slices.Index(root, "root@1.2.3.4") {
		t.Fatalf("root ssh argümanları %q", root)
	}
	if slices.Contains(read, "StrictHostKeyChecking=yes") {
		t.Fatalf("okuma komutuna seçenek eklendi: %q", read)
	}
	if !slices.Equal(read[1:], []string{"-T", "root@1.2.3.4", "true"}) {
		t.Fatalf("okuma argümanları değişti: %q", read)
	}
}

func TestMaskedEnvRefused(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	for _, line := range []string{"app create -env K=*** web", "app update -env=K=*** web", "app create --env K=*** web", "app update -env A=1 -env B=*** web"} {
		id, exit := h.exec("hetzner", line)
		if exit == cli.ExitOK || !strings.Contains(h.output(id, StreamErr), "this line has hidden values from history; type them again") {
			t.Errorf("%s: exit %d stderr %q", line, exit, h.output(id, StreamErr))
		}
	}
	// "="siz *** zaten -env'in kendi doğrulamasında (KEY=VALUE) reddedilir.
	if _, exit := h.exec("hetzner", "app create -env *** web"); exit == cli.ExitOK {
		t.Error("app create -env *** web çalıştı")
	}
	if h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatal("gizli değer taşıyan satır ilerledi")
	}
	if _, exit := h.exec("hetzner", "app create -env K=x*** web"); exit != cli.ExitOK {
		t.Fatalf("yalnız *** olmayan değer reddedilmemeli: exit %d", exit)
	}
}

func TestUnknownHostHint(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	h.setRun(func(_ context.Context, _ []string, env cli.Env) int {
		_, _ = env.Stderr.Write([]byte("Host key verifi"))
		_, _ = env.Stderr.Write([]byte("cation failed.\r\n"))
		return cli.ExitError
	})
	hint := "connect once from the CLI first (kadran key list -s hetzner)"
	id := h.start("hetzner", "key list")
	h.waitInput(id)
	if err := h.r.Input(id, "hetzner"); err != nil {
		t.Fatal(err)
	}
	h.done(id)
	if !strings.Contains(h.output(id, StreamErr), hint) {
		t.Fatalf("root stderr %q", h.output(id, StreamErr))
	}
	id, _ = h.exec("hetzner", "status")
	if strings.Contains(h.output(id, ""), hint) {
		t.Fatal("okuma komutunda ipucu")
	}
}

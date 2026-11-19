package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
	"github.com/erkanrzgc/kadran/desktop/internal/console"
	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// consoleHarness, gerçek bir console.Runner'ı sahte Run ile kurar; onay
// kutusu köprünün Host.Confirm'ıdır (masaüstündeki bağlamayla aynı).
type consoleHarness struct {
	*harness
	runs atomic.Int32
	done chan console.DoneData
	// onRun doluysa sahte Run onu çalıştırır (örn. stdin okuyup bekler).
	onRun  func(cli.Env) int
	inputs chan console.InputData
	mu     sync.Mutex
	evs    []console.Event
}

func newConsoleHarness(t *testing.T, answer, fixture bool) *consoleHarness {
	t.Helper()
	ch := &consoleHarness{harness: newHarness(t, answer), done: make(chan console.DoneData, 8), inputs: make(chan console.InputData, 8)}
	runner := console.NewRunner(console.RunnerDeps{
		Run: func(_ context.Context, _ []string, env cli.Env) int {
			ch.runs.Add(1)
			if ch.onRun != nil {
				return ch.onRun(env)
			}
			return 0
		},
		Confirm: ch.h.Confirm,
		Emit: func(e console.Event) {
			ch.mu.Lock()
			ch.evs = append(ch.evs, e)
			ch.mu.Unlock()
			switch d := e.Data.(type) {
			case console.DoneData:
				ch.done <- d
			case console.InputData:
				ch.inputs <- d
			}
		},
		History: console.History{Dir: ch.dir},
		Admin:   console.AdminSetting{Dir: ch.dir},
		Log:     console.AdminLog{Dir: ch.dir},
		Servers: func() ([]servers.Server, error) {
			return []servers.Server{
				{Name: "web1", Target: "kadran-client@203.0.113.7"},
				{Name: "fake", Target: fixtureTarget},
			}, nil
		},
	})
	ch.b = New(Deps{
		Dispatcher: ch.d,
		Host:       ch.h,
		Settings:   appearance.Store{Dir: ch.dir},
		Material:   func() appearance.Material { return appearance.Accent },
		Fixture:    fixture,
		Console:    runner,
	})
	t.Cleanup(runner.Shutdown)
	return ch
}

func (ch *consoleHarness) waitDone(t *testing.T) console.DoneData {
	t.Helper()
	select {
	case d := <-ch.done:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("console:done gelmedi")
		return console.DoneData{}
	}
}

func TestConsoleRunNeverRunsUnconfirmed(t *testing.T) {
	ch := newConsoleHarness(t, false, false)
	id, err := ch.b.ConsoleRun("web1", "deploy web")
	if err != nil || id == "" {
		t.Fatalf("%q %v", id, err)
	}
	if d := ch.waitDone(t); d.Exit == 0 {
		t.Errorf("onaysız komut 0 ile bitti: %+v", d)
	}
	if ch.runs.Load() != 0 || ch.h.boxCount() != 1 {
		t.Errorf("çalışma %d, kutu %d", ch.runs.Load(), ch.h.boxCount())
	}
}

// Duman bayrağı konsolun kutusunu atlatmaz: konsolun tek onay yolu Host.Confirm.
// Atlatma yalnız fake@test hedefinde var (confirmed); test o hedefle koşar.
func TestConsoleFixtureDoesNotSkipConfirm(t *testing.T) {
	ch := newConsoleHarness(t, false, true)
	if _, err := ch.b.ConsoleRun("fake", "deploy web"); err != nil {
		t.Fatal(err)
	}
	ch.waitDone(t)
	if ch.h.boxCount() != 1 || ch.runs.Load() != 0 {
		t.Errorf("kutu %d, çalışma %d", ch.h.boxCount(), ch.runs.Load())
	}
}

func TestConsoleRunReadLineRuns(t *testing.T) {
	ch := newConsoleHarness(t, false, false)
	if _, err := ch.b.ConsoleRun("web1", "status"); err != nil {
		t.Fatal(err)
	}
	if d := ch.waitDone(t); d.Exit != 0 || ch.runs.Load() != 1 || ch.h.boxCount() != 0 {
		t.Errorf("%+v %d %d", d, ch.runs.Load(), ch.h.boxCount())
	}
}

func TestConsoleSetAdminOnlyViaHost(t *testing.T) {
	ch := newConsoleHarness(t, false, false)
	if _, err := ch.b.SaveAppearance(json.RawMessage(`{"version":2,"consoleAdmin":true,"console-admin":true,"admin":true}`)); err != nil {
		t.Fatal(err)
	}
	st, err := ch.b.ConsoleState("web1")
	if err != nil || st.Admin {
		t.Fatalf("görünüm ayarı yöneticiyi açtı: %+v %v", st, err)
	}
	// Kutu reddedilirse açılmaz; onaylanırsa açılır; kapatmak onaysız.
	if on, err := ch.b.ConsoleSetAdmin(true); err != nil || on {
		t.Fatalf("reddedilen kutu açtı: %v %v", on, err)
	}
	ch.h.answer = true
	if on, err := ch.b.ConsoleSetAdmin(true); err != nil || !on {
		t.Fatalf("onaylı açılmadı: %v %v", on, err)
	}
	boxes := ch.h.boxCount()
	if on, err := ch.b.ConsoleSetAdmin(false); err != nil || on || ch.h.boxCount() != boxes {
		t.Fatalf("kapatma kutu istedi ya da kapanmadı: %v %v", on, err)
	}
}

func TestConsoleGuards(t *testing.T) {
	ch := newConsoleHarness(t, true, false)
	b := ch.b
	long := strings.Repeat("a", 4097)
	bad := []struct {
		name string
		err  func() error
	}{
		{"line too long", func() error { _, e := b.ConsoleRun("web1", long); return e }},
		{"server too long", func() error { _, e := b.ConsoleRun(strings.Repeat("s", 201), "status"); return e }},
		{"complete line too long", func() error { _, e := b.ConsoleComplete("web1", long, 0); return e }},
		{"complete cursor", func() error { _, e := b.ConsoleComplete("web1", "sta", 9); return e }},
		{"state server", func() error { _, e := b.ConsoleState(strings.Repeat("s", 201)); return e }},
	}
	for _, c := range bad {
		if err := c.err(); err == nil {
			t.Errorf("%s: hata yok", c.name)
		}
	}
	if ch.runs.Load() != 0 || ch.h.boxCount() != 0 {
		t.Errorf("Runner çağrıldı: %d %d", ch.runs.Load(), ch.h.boxCount())
	}
	// Çalıştırma hiç başlamadıysa Runner meşgul değildir: sonraki satır koşar.
	if _, err := b.ConsoleRun("web1", "status"); err != nil {
		t.Fatalf("guard reddi Runner'ı meşgul bıraktı: %v", err)
	}
	ch.waitDone(t)
}

func TestConsoleMissingRunnerFails(t *testing.T) {
	hs := newHarness(t, true)
	if _, err := hs.b.ConsoleRun("web1", "status"); err == nil {
		t.Error("Runner yokken hata dönmedi")
	}
	if _, err := hs.b.ConsoleState("web1"); err == nil {
		t.Error("Runner yokken hata dönmedi")
	}
}

func TestConsoleCompleteAndState(t *testing.T) {
	ch := newConsoleHarness(t, true, false)
	got, err := ch.b.ConsoleComplete("web1", "sta", 3)
	if err != nil || len(got) == 0 || got[0] != "status" {
		t.Fatalf("%v %v", got, err)
	}
	st, err := ch.b.ConsoleState("")
	if err != nil || st.History == nil {
		t.Fatalf("%+v %v", st, err)
	}
}

// Kötü girdi etkin bir çalıştırmaya verilirse hata döner VE çalıştırma hâlâ
// istem bekler: ardından gelen geçerli satır onu tamamlar.
func TestConsoleInputGuardsKeepRunWaiting(t *testing.T) {
	ch := newConsoleHarness(t, true, false)
	got := make(chan string, 1)
	ch.onRun = func(env cli.Env) int {
		line, _ := bufio.NewReader(env.Stdin).ReadString('\n')
		got <- strings.TrimSpace(line)
		return 0
	}
	id, err := ch.b.ConsoleRun("web1", "status")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch.inputs:
	case <-time.After(5 * time.Second):
		t.Fatal("komut istem beklemiyor")
	}
	for name, text := range map[string]string{"long": strings.Repeat("a", 257), "bidi": "x\u202ey", "newline": "x\ny"} {
		if err := ch.b.ConsoleInput(id, text); err == nil {
			t.Errorf("%s: hata yok", name)
		}
	}
	select {
	case v := <-got:
		t.Fatalf("kötü girdi komuta ulaştı: %q", v)
	default:
	}
	if err := ch.b.ConsoleInput(id, "ok"); err != nil {
		t.Fatalf("geçerli satır reddedildi: %v", err)
	}
	if v := <-got; v != "ok" {
		t.Errorf("komut %q aldı", v)
	}
	if d := ch.waitDone(t); d.Exit != 0 {
		t.Errorf("%+v", d)
	}
}

// Kötü runID Runner a ulaşmadan reddedilir: hata ErrNoRun DEĞİL (o Runner
// cevabı olurdu), guard cevabı.
func TestConsoleMalformedRunIDRejectedByGuard(t *testing.T) {
	ch := newConsoleHarness(t, true, false)
	for _, id := range []string{"../x", "0123456789abcde", "0123456789abcdef0", "0123456789ABCDEF", ""} {
		for name, err := range map[string]error{"input": ch.b.ConsoleInput(id, "ok"), "cancel": ch.b.ConsoleCancel(id)} {
			if err == nil || errors.Is(err, console.ErrNoRun) {
				t.Errorf("%s %q: guard tetiklenmedi (%v)", name, id, err)
			}
		}
	}
}

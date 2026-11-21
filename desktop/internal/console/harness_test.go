package console

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/servers"
)

const waitLimit = 5 * time.Second

// runCall, sahte Run'ın bir çağrısı.
type runCall struct {
	args []string
	ctx  context.Context
}

// harness, çalıştırıcıyı sahte Run, sahte Confirm ve olay toplayıcıyla kurar.
type harness struct {
	t   *testing.T
	r   *Runner
	dir string

	mu       sync.Mutex
	events   []Event
	calls    []runCall
	boxes    []confirm.Box
	answer   bool
	answerFn func(confirm.Box) (bool, error)
	fakeRun  func(ctx context.Context, args []string, env cli.Env) int
	list     []servers.Server
	signal   chan struct{}
	started  chan struct{}
}

func testServers() []servers.Server {
	return []servers.Server{
		{Name: "hetzner", Target: "kadran-client@1.2.3.4", Admin: "root@1.2.3.4"},
		{Name: "gcp", Target: "kadran-client@5.6.7.8", Admin: "ops@5.6.7.8", Sudo: true},
		{Name: "bare", Target: "kadran-client@9.9.9.9"},
	}
}

func newHarness(t *testing.T, edit ...func(*RunnerDeps)) *harness {
	t.Helper()
	h := &harness{t: t, dir: t.TempDir(), list: testServers(), signal: make(chan struct{}, 1), started: make(chan struct{}, 16)}
	deps := RunnerDeps{
		Run:      h.run,
		Confirm:  h.confirm,
		Emit:     h.emit,
		History:  History{Dir: h.dir},
		Admin:    AdminSetting{Dir: h.dir},
		Log:      AdminLog{Dir: h.dir},
		Servers:  h.servers,
		Language: func() confirm.Language { return confirm.English },
		Now:      time.Now,
	}
	for _, e := range edit {
		e(&deps)
	}
	h.r = NewRunner(deps)
	t.Cleanup(h.r.Shutdown)
	return h
}

func (h *harness) run(ctx context.Context, args []string, env cli.Env) int {
	h.mu.Lock()
	h.calls = append(h.calls, runCall{args: slices.Clone(args), ctx: ctx})
	fake := h.fakeRun
	h.mu.Unlock()
	select {
	case h.started <- struct{}{}:
	default:
	}
	if fake == nil {
		return cli.ExitOK
	}
	return fake(ctx, args, env)
}

func (h *harness) confirm(box confirm.Box) (bool, error) {
	h.mu.Lock()
	h.boxes = append(h.boxes, box)
	answer, fn := h.answer, h.answerFn
	h.mu.Unlock()
	if fn != nil {
		return fn(box)
	}
	return answer, nil
}

func (h *harness) emit(e Event) {
	h.mu.Lock()
	h.events = append(h.events, e)
	h.mu.Unlock()
	select {
	case h.signal <- struct{}{}:
	default:
	}
}

func (h *harness) servers() ([]servers.Server, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.list), nil
}

func (h *harness) setAnswer(ok bool) {
	h.mu.Lock()
	h.answer = ok
	h.mu.Unlock()
}

func (h *harness) setRun(fn func(ctx context.Context, args []string, env cli.Env) int) {
	h.mu.Lock()
	h.fakeRun = fn
	h.mu.Unlock()
}

func (h *harness) callCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.calls)
}

func (h *harness) lastCall() runCall {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.calls) == 0 {
		h.t.Fatal("Run hiç çağrılmadı")
	}
	return h.calls[len(h.calls)-1]
}

func (h *harness) boxCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.boxes)
}

func (h *harness) lastBox() confirm.Box {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.boxes) == 0 {
		h.t.Fatal("onay kutusu gösterilmedi")
	}
	return h.boxes[len(h.boxes)-1]
}

func (h *harness) snapshot() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.events)
}

// waitEvent, koşulu sağlayan ilk olayı bekler.
func (h *harness) waitEvent(what string, match func(Event) bool) Event {
	h.t.Helper()
	deadline := time.After(waitLimit)
	for {
		for _, e := range h.snapshot() {
			if match(e) {
				return e
			}
		}
		select {
		case <-h.signal:
		case <-deadline:
			h.t.Fatalf("%s gelmedi; olaylar: %+v", what, h.snapshot())
		}
	}
}

func (h *harness) start(server, line string) string {
	h.t.Helper()
	id, err := h.r.Run(server, line)
	if err != nil {
		h.t.Fatalf("Run(%q): %v", line, err)
	}
	return id
}

func (h *harness) done(id string) int {
	h.t.Helper()
	e := h.waitEvent("done "+id, func(e Event) bool {
		d, ok := e.Data.(DoneData)
		return e.Name == EventDone && ok && d.Run == id
	})
	d, _ := e.Data.(DoneData)
	return d.Exit
}

// exec, satırı seçili sunucuda çalıştırır ve çıkış kodunu döner.
func (h *harness) exec(server, line string) (string, int) {
	h.t.Helper()
	id := h.start(server, line)
	return id, h.done(id)
}

func (h *harness) waitInput(id string) InputData {
	h.t.Helper()
	e := h.waitEvent("input "+id, func(e Event) bool {
		d, ok := e.Data.(InputData)
		return e.Name == EventInput && ok && d.Run == id
	})
	d, _ := e.Data.(InputData)
	return d
}

// output, çalıştırmanın çıktısını sırasıyla birleştirir (stream boşsa ikisi).
func (h *harness) output(id, stream string) string {
	var b strings.Builder
	for _, e := range h.snapshot() {
		d, ok := e.Data.(OutputData)
		if e.Name == EventOutput && ok && d.Run == id && (stream == "" || d.Stream == stream) {
			b.WriteString(d.Data)
		}
	}
	return b.String()
}

func (h *harness) waitStarted() {
	h.t.Helper()
	select {
	case <-h.started:
	case <-time.After(waitLimit):
		h.t.Fatal("Run başlamadı")
	}
}

func boxText(b confirm.Box) string {
	return strings.Join([]string{b.Title, b.Message, b.Detail, b.Action}, "\n")
}

func (h *harness) history(server string) []string {
	return h.r.State(server).History
}

// blockUntilCancelled, bağlam iptal edilene ya da release kapanana dek
// bekleyen sahte Run kurar.
func blockUntilCancelled(h *harness, release chan struct{}) {
	h.setRun(func(ctx context.Context, _ []string, _ cli.Env) int {
		select {
		case <-ctx.Done():
			return cli.ExitError
		case <-release:
			return cli.ExitOK
		}
	})
}

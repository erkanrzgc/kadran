package bridge

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/sidecar"
)

type call struct {
	method string
	params map[string]any
}

// fakeDispatcher, çağrıları kaydeder ve hazır yanıt döner.
type fakeDispatcher struct {
	mu      sync.Mutex
	calls   []call
	fail    *sidecar.Error
	results map[string]any
}

func (f *fakeDispatcher) Dispatch(_ context.Context, method string, params json.RawMessage) (any, *sidecar.Error) {
	var p map[string]any
	_ = json.Unmarshal(params, &p)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call{method: method, params: p})
	if f.fail != nil {
		return nil, f.fail
	}
	if r, ok := f.results[method]; ok {
		return r, nil
	}
	return map[string]any{"ok": method}, nil
}

func (f *fakeDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// fakeHost, onay kutularını kaydeder; hold açıksa kutu, release kapanana
// dek yanıt vermez.
type fakeHost struct {
	mu        sync.Mutex
	boxes     []confirm.Box
	answer    bool
	hold      chan struct{}
	shown     chan struct{}
	looks     []appearance.WindowLook
	clipboard string
	actions   []string
	// onLook, SetLook çağrıldığı anda koşar (sıra testleri için).
	onLook func()
}

func (h *fakeHost) Minimise()       { h.actions = append(h.actions, "minimise") }
func (h *fakeHost) ToggleMaximise() { h.actions = append(h.actions, "maximise") }
func (h *fakeHost) Close()          { h.actions = append(h.actions, "close") }
func (h *fakeHost) SetLook(l appearance.WindowLook) {
	if h.onLook != nil {
		h.onLook()
	}
	h.looks = append(h.looks, l)
}
func (h *fakeHost) SetClipboard(text string) error {
	h.clipboard = text
	return nil
}
func (h *fakeHost) Confirm(box confirm.Box) (bool, error) {
	h.mu.Lock()
	h.boxes = append(h.boxes, box)
	hold, shown := h.hold, h.shown
	h.mu.Unlock()
	if shown != nil {
		shown <- struct{}{}
	}
	if hold != nil {
		<-hold
	}
	return h.answer, nil
}

func (h *fakeHost) boxCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.boxes)
}

type harness struct {
	b    *Bridge
	d    *fakeDispatcher
	h    *fakeHost
	dir  string
	warn []string
}

func newHarness(t *testing.T, answer bool) *harness {
	t.Helper()
	hs := &harness{d: &fakeDispatcher{results: map[string]any{}}, h: &fakeHost{answer: answer}, dir: t.TempDir()}
	hs.b = New(Deps{
		Dispatcher: hs.d,
		Host:       hs.h,
		Settings:   appearance.Store{Dir: hs.dir},
		Material:   func() appearance.Material { return appearance.Accent },
		Warn:       func(m string) { hs.warn = append(hs.warn, m) },
	})
	return hs
}

func (hs *harness) language(t *testing.T, lang string) {
	t.Helper()
	if _, err := hs.b.deps.Settings.Save(json.RawMessage(`{"version":2,"language":"` + lang + `"}`)); err != nil {
		t.Fatal(err)
	}
}

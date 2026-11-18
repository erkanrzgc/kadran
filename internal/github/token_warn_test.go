package github

import (
	"context"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type countingHandler struct{ warns *atomic.Int32 }

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		h.warns.Add(1)
	}
	return nil
}
func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

func TestTransientRefreshWarnsAtMostOncePerMinute(t *testing.T) {
	var warns atomic.Int32
	old := slog.Default()
	slog.SetDefault(slog.New(countingHandler{&warns}))
	t.Cleanup(func() { slog.SetDefault(old) })

	now := tokenTestNow
	tk, _ := newTestTokens(t, expiringState(), func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	tk.now = func() time.Time { return now }
	tk.st.AccessExpiresAt = tokenTestNow.Add(4 * time.Minute)
	for i := 0; i < 3; i++ {
		if _, err := tk.Token(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if warns.Load() != 1 {
		t.Fatalf("warnings = %d, want 1 within a minute", warns.Load())
	}
	now = now.Add(61 * time.Second)
	if _, err := tk.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if warns.Load() != 2 {
		t.Fatalf("warnings = %d, want 2 after a minute", warns.Load())
	}
}

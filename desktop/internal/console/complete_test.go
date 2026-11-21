package console

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// appsFake, uygulama adlarını veren sahte ve çağrı sayacı.
type appsFake struct {
	mu    sync.Mutex
	calls int
}

func (a *appsFake) apps(_ context.Context, server string) ([]string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	if server != "hetzner" {
		return nil, nil
	}
	return []string{"worker", "web", "api"}, nil
}

func (a *appsFake) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls
}

// stepClock, elle ilerletilen saat.
type stepClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *stepClock) add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func completeHarness(t *testing.T) (*harness, *appsFake, *stepClock) {
	apps := &appsFake{}
	clock := &stepClock{now: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)}
	h := newHarness(t, func(d *RunnerDeps) {
		d.Apps = apps.apps
		d.Now = clock.Now
	})
	return h, apps, clock
}

func complete(h *harness, line string) []string {
	return h.r.Complete("hetzner", line, len(line))
}

func TestCompleteCandidates(t *testing.T) {
	h, apps, clock := completeHarness(t)

	if got := complete(h, "de"); !slices.Equal(got, []string{"deploy"}) {
		t.Fatalf("de: %q", got)
	}
	if got := complete(h, "app c"); !slices.Equal(got, []string{"create"}) {
		t.Fatalf("app c: %q", got)
	}
	flags := complete(h, "deploy -")
	for _, want := range []string{"-branch", "-commit"} {
		if !slices.Contains(flags, want) {
			t.Fatalf("deploy -: %q", flags)
		}
	}
	for _, f := range flags {
		if !strings.HasPrefix(f, "-") || f == "-s" {
			t.Fatalf("deploy -: %q", flags)
		}
	}
	if got := complete(h, "deploy w"); !slices.Equal(got, []string{"web", "worker"}) {
		t.Fatalf("deploy w: %q", got)
	}
	if got := complete(h, "rollback "); !slices.Equal(got, []string{"api", "web", "worker"}) {
		t.Fatalf("rollback: %q", got)
	}
	if n := apps.count(); n != 1 {
		t.Fatalf("30 sn içinde %d Apps çağrısı, 1 olmalı", n)
	}
	clock.add(31 * time.Second)
	complete(h, "deploy w")
	if n := apps.count(); n != 2 {
		t.Fatalf("bayat önbellek yenilenmedi: %d çağrı", n)
	}
	if got := complete(h, "use "); !slices.Equal(got, []string{"bare", "gcp", "hetzner"}) {
		t.Fatalf("use: %q", got)
	}
	if got := complete(h, "use g"); !slices.Equal(got, []string{"gcp"}) {
		t.Fatalf("use g: %q", got)
	}
}

func TestCompleteSkipsBannedAndValues(t *testing.T) {
	h, apps, _ := completeHarness(t)
	if got := complete(h, "sid"); len(got) != 0 {
		t.Fatalf("yasak komut önerildi: %q", got)
	}
	if got := complete(h, "b"); slices.Contains(got, "bootstrap") || !slices.Contains(got, "backup") {
		t.Fatalf("b: %q", got)
	}
	if got := complete(h, "server "); !slices.Equal(got, []string{"list"}) {
		t.Fatalf("server: %q", got)
	}
	if got := complete(h, "cl"); !slices.Equal(got, []string{"clear"}) {
		t.Fatalf("cl: %q", got)
	}
	if got := complete(h, "deploy -branch "); len(got) != 0 {
		t.Fatalf("bayrak değeri yerine aday: %q", got)
	}
	if got := complete(h, "deploy web "); len(got) != 0 {
		t.Fatalf("ikinci konumsal için aday: %q", got)
	}
	if got := complete(h, "app create "); len(got) != 0 {
		t.Fatalf("yeni uygulama adı için aday: %q", got)
	}
	if got := complete(h, "kadran dep"); !slices.Equal(got, []string{"deploy"}) {
		t.Fatalf("kadran dep: %q", got)
	}
	if n := apps.count(); n != 0 {
		t.Fatalf("gereksiz Apps çağrısı: %d", n)
	}
}

func TestCompleteUsesCursor(t *testing.T) {
	h, _, _ := completeHarness(t)
	if got := h.r.Complete("hetzner", "de web", 2); !slices.Equal(got, []string{"deploy"}) {
		t.Fatalf("imleç ortada: %q", got)
	}
	// İmleç JavaScript'in UTF-16 birimiyle gelir; "ğ" bir birim, iki bayt.
	// Bayt sayılsaydı imleç "w"den önce kalır, üç uygulama birden önerilirdi.
	line := "deploy -branch=ğ w"
	if got := h.r.Complete("hetzner", line, 18); !slices.Equal(got, []string{"web", "worker"}) {
		t.Fatalf("UTF-16 imleci: %q", got)
	}
	for _, cursor := range []int{-5, 999} {
		_ = h.r.Complete("hetzner", "de", cursor)
	}
	if got := h.r.Complete("", "deploy w", 8); len(got) != 0 {
		t.Fatalf("sunucusuz uygulama adı: %q", got)
	}
	if got := h.r.Complete("nowhere", "deploy w", 8); len(got) != 0 {
		t.Fatalf("kayıtsız sunucu için uygulama adı: %q", got)
	}
}

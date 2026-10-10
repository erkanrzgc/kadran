package health

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/deploy"
	"github.com/erkanrzgc/kadran/internal/store"
)

type observedHealer struct {
	checks    atomic.Int32
	heals     atomic.Int32
	checked   chan struct{}
	healedApp store.App
}

func (h *observedHealer) Check(context.Context, store.App, string) (uint32, string) {
	if h.checks.Add(1) == 1 && h.checked != nil {
		close(h.checked)
	}
	return 0, "not ready"
}
func (h *observedHealer) Heal(_ context.Context, app store.App, _ store.Release) (bool, error) {
	h.heals.Add(1)
	h.healedApp = app
	return false, nil
}

func TestHealHonorsSharedQueueAndCancellation(t *testing.T) {
	c := deploy.NewCoordinator()
	owner, _ := c.Acquire(context.Background(), testApp)
	defer owner()
	opts := DefaultOptions
	opts.Coordinator = c
	opts.FailuresBeforeHeal = 1
	st := &fakeStore{deps: []store.Deployment{{AppID: testApp, ReleaseID: testRel}}}
	h := &observedHealer{checked: make(chan struct{})}
	sup, err := New(h, st, &fakeAuditor{}, &fakeAlarms{}, &fakeClock{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { sup.Cycle(ctx); close(done) }()
	<-h.checked
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("queued heal ignored cancellation")
	}
	if h.heals.Load() != 0 {
		t.Fatal("heal entered while another lifecycle operation owned the app")
	}
}

type changingDefinitionStore struct {
	*fakeStore
	reads int
}

func (s *changingDefinitionStore) GetApp(ctx context.Context, id string) (store.App, error) {
	app, err := s.fakeStore.GetApp(ctx, id)
	s.reads++
	if s.reads >= 2 {
		app.Replicas = 2
		app.HealthPath = "/new-health"
	}
	return app, err
}

func TestHealRefreshesAppDefinitionAfterAcquiringQueue(t *testing.T) {
	opts := DefaultOptions
	opts.FailuresBeforeHeal = 1
	st := &changingDefinitionStore{fakeStore: &fakeStore{deps: []store.Deployment{{AppID: testApp, ReleaseID: testRel}}}}
	h := &observedHealer{}
	sup, err := New(h, st, &fakeAuditor{}, &fakeAlarms{}, &fakeClock{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	sup.Cycle(context.Background())
	if h.healedApp.Replicas != 2 || h.healedApp.HealthPath != "/new-health" {
		t.Fatalf("heal used a stale definition: %+v", h.healedApp)
	}
}

func TestBusyAppDoesNotStarveAnotherAppsHealing(t *testing.T) {
	c := deploy.NewCoordinator()
	owner, _ := c.Acquire(context.Background(), testApp)
	defer owner()
	opts := DefaultOptions
	opts.Coordinator = c
	opts.FailuresBeforeHeal = 1
	opts.Interval = 15 * time.Millisecond
	st := &fakeStore{deps: []store.Deployment{{AppID: testApp, ReleaseID: testRel}, {AppID: "shop", ReleaseID: "r1"}}}
	h := &observedHealer{}
	sup, err := New(h, st, &fakeAuditor{}, &fakeAlarms{}, &fakeClock{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { sup.Cycle(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("busy app starved another app's supervision")
	}
	if h.heals.Load() != 1 || h.healedApp.ID != "shop" {
		t.Fatalf("wrong app healed while first app busy: count=%d, app=%s", h.heals.Load(), h.healedApp.ID)
	}
}

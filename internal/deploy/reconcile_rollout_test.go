package deploy

import (
	"context"
	"errors"
	"testing"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
	"github.com/erkanrzgc/kadran/internal/store"
)

// The active deployment changes at the real rollout's activation boundary.
// External Docker state then changes before the real reconciler lists it.
type activationBoundary struct {
	app           store.App
	active        string
	afterActivate func()
}

func (a *activationBoundary) SetActiveRelease(_ context.Context, _, release string) error {
	a.active = release
	a.afterActivate()
	return nil
}

func (a *activationBoundary) ActiveDeployments(context.Context) ([]store.Deployment, error) {
	return []store.Deployment{{AppID: a.app.ID, ReleaseID: a.active, Domain: a.app.Domain, ContainerPort: a.app.ContainerPort, Replicas: a.app.Replicas}}, nil
}

func TestRolloutSkippedAfterReadinessKeepsPreviousLiveRoute(t *testing.T) {
	for _, trigger := range []string{"new replica disappears", "container listing fails"} {
		t.Run(trigger, func(t *testing.T) {
			app := testApplication()
			life := &fakeLifecycle{replicas: bothReleasesUp()}
			previous := canli(t, rota(app.ID, app.Domain, "172.20.0.2:8080"))
			proxy := &fakeProxy{live: previous}
			activation := &activationBoundary{app: app, active: relOld, afterActivate: func() {
				if trigger == "container listing fails" {
					life.listErr = errors.New("executor temporarily unavailable")
				} else {
					life.replicas = life.replicas[:1]
				}
			}}
			reconciler := mustReconciler(t, activation, life, proxy)
			rollout, err := NewRollout(life, activation, reconciler, &fakeProber{}, DefaultGate, DefaultDrain)
			if err != nil {
				t.Fatal(err)
			}
			rollout.clock = &fakeClock{}
			err = rollout.Run(context.Background(), app, store.Release{ID: relNew})
			var skipped SkippedError
			if !errors.As(err, &skipped) || skipped.Result.Skipped[app.ID] == "" {
				t.Fatalf("missing own route did not fail the rollout: %v", err)
			}
			if activation.active != relNew {
				t.Fatal("trigger did not reach the post-readiness activation boundary")
			}
			if len(life.stopped) != 0 {
				t.Fatal("failed rollout drained the previous release")
			}
			if proxy.calls != 1 || proxydrv.Matches(previous, proxy.loaded) != nil {
				t.Fatalf("failed rollout removed or changed the previous live route: %v", hosts(t, proxy.loaded))
			}
		})
	}
}

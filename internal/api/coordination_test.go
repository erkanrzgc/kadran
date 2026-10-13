package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/execclient"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// drainRollout models the observed failure at the real operation boundary:
// stale release IDs are captured, traffic moves, drain waits, then IDs stop.
// Both callers share the same actual database and server handlers.
type drainRollout struct {
	db              *store.Store
	started         chan struct{}
	release         chan struct{}
	rollbackStarted chan struct{}
	mu              sync.Mutex
	stopped         map[string]bool
}

func TestEveryAppMutationHonorsCanceledQueue(t *testing.T) {
	for _, action := range []string{"deploy", "rollback", "update", "delete", "prune"} {
		t.Run(action, func(t *testing.T) {
			ro := &fakeRollout{}
			srv := newRollbackServer(t, ro)
			deployN(t, srv, 2)
			owner, err := srv.coordinator.Acquire(context.Background(), "blog")
			if err != nil {
				t.Fatal(err)
			}
			defer owner()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
			defer cancel()
			switch action {
			case "deploy":
				err = srv.Deploy(&kadranv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, newDeployStream(ctx))
			case "rollback":
				_, err = srv.Rollback(ctx, &kadranv1.RollbackRequest{AppId: "blog"})
			case "update":
				branch := "changed"
				_, err = srv.UpdateApp(ctx, &kadranv1.UpdateAppRequest{AppId: "blog", GitBranch: &branch})
			case "delete":
				_, err = srv.DeleteApp(ctx, &kadranv1.DeleteAppRequest{AppId: "blog"})
			case "prune":
				_, err = srv.PruneApp(ctx, &kadranv1.PruneAppRequest{AppId: "blog"})
			}
			if status.Code(err) != codes.DeadlineExceeded {
				t.Fatalf("queue cancellation code = %v, err=%v", status.Code(err), err)
			}
			if len(ro.calls) != 2 || len(ro.rollbackCalls) != 0 {
				t.Fatal("canceled queued operation changed lifecycle state")
			}
			app, err := srv.store.GetApp(context.Background(), "blog")
			if err != nil || app.ReleaseSeq != 2 || app.GitBranch != "main" {
				t.Fatalf("canceled operation changed stored app: %+v, %v", app, err)
			}
			fe, ok := srv.exec.(*fakeExec)
			if !ok {
				t.Fatal("fixture did not install the expected executor")
			}
			if len(fe.rmCalls) != 0 || len(fe.stopCalls) != 0 || fe.buildCalls != 2 {
				t.Fatal("canceled operation reached executor")
			}
		})
	}
}

// Done is first evaluated by Acquire after its waiter has joined the queue.
// It lets state-refresh tests make the preceding operation's changes while
// holding ownership, without scheduling sleeps or racing mutable fake state.
type waitingContext struct {
	context.Context
	queued chan struct{}
	once   sync.Once
}

func (c *waitingContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.queued) })
	return c.Context.Done()
}

func TestRollbackReadsHistoryAfterItsQueueTurn(t *testing.T) {
	srv := newRollbackServer(t, &fakeRollout{})
	deployN(t, srv, 2)
	owner, _ := srv.coordinator.Acquire(context.Background(), "blog")
	ctx := &waitingContext{Context: context.Background(), queued: make(chan struct{})}
	type result struct {
		resp *kadranv1.RollbackResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := srv.Rollback(ctx, &kadranv1.RollbackRequest{AppId: "blog"})
		done <- result{resp, err}
	}()
	<-ctx.queued
	rel, err := srv.store.StartRelease(context.Background(), "blog", apiSHA)
	if err != nil {
		owner()
		t.Fatal(err)
	}
	if err := srv.store.FinishRelease(context.Background(), "blog", rel.ID, "sha256:test"); err != nil {
		owner()
		t.Fatal(err)
	}
	activate(t, srv.store, "blog", rel.ID)
	owner()
	got := <-done
	if got.err != nil {
		t.Fatal(got.err)
	}
	if got.resp.GetFromReleaseId() != "r3" || got.resp.GetToReleaseId() != "r2" {
		t.Fatalf("rollback used stale history: %+v", got.resp)
	}
}

func TestPruneRefreshesProtectedReleasesAfterItsQueueTurn(t *testing.T) {
	fe := &fakeExec{replicas: []execclient.Replica{replica("blog", "r1", 0), replica("blog", "r2", 0), replica("blog", "r3", 0)}}
	srv, db := newDeleteServer(t, fe)
	seedReleases(t, db, "blog", 3)
	activate(t, db, "blog", "r1", "r2")
	owner, _ := srv.coordinator.Acquire(context.Background(), "blog")
	ctx := &waitingContext{Context: context.Background(), queued: make(chan struct{})}
	done := make(chan error, 1)
	go func() { _, err := srv.PruneApp(ctx, &kadranv1.PruneAppRequest{AppId: "blog"}); done <- err }()
	<-ctx.queued
	activate(t, db, "blog", "r3")
	owner()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(fe.rmCalls, ","); got != "blog/r1" {
		t.Fatalf("prune removed new live/rollback releases: %s", got)
	}
}

func (r *drainRollout) Run(ctx context.Context, app store.App, rel store.Release) error {
	old, err := r.db.ActiveDeployment(ctx, app.ID)
	if err != nil {
		return err
	}
	if err := r.db.SetActiveRelease(ctx, app.ID, rel.ID); err != nil {
		return err
	}
	close(r.started)
	select {
	case <-r.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	r.mu.Lock()
	r.stopped[old.ReleaseID] = true
	r.mu.Unlock()
	return nil
}

func (r *drainRollout) Rollback(ctx context.Context, app store.App, rel store.Release) (bool, error) {
	close(r.rollbackStarted)
	old, err := r.db.ActiveDeployment(ctx, app.ID)
	if err != nil {
		return false, err
	}
	if err := r.db.SetActiveRelease(ctx, app.ID, rel.ID); err != nil {
		return false, err
	}
	r.mu.Lock()
	r.stopped[rel.ID] = false
	r.stopped[old.ReleaseID] = true
	r.mu.Unlock()
	return false, nil
}

func TestRollbackDuringDeployDrainKeepsActiveReleaseRunning(t *testing.T) {
	srv := newRollbackServer(t, &fakeRollout{})
	deployN(t, srv, 2)
	ro := &drainRollout{db: srv.store, started: make(chan struct{}), release: make(chan struct{}), rollbackStarted: make(chan struct{}), stopped: map[string]bool{}}
	srv.rollout = ro
	done := make(chan error, 1)
	go func() {
		done <- srv.Deploy(&kadranv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, newDeployStream(context.Background()))
	}()
	<-ro.started
	rbDone := make(chan error, 1)
	go func() {
		_, err := srv.Rollback(context.Background(), &kadranv1.RollbackRequest{AppId: "blog"})
		rbDone <- err
	}()
	// A rollback started during drain must wait for the whole deployment.
	select {
	case <-ro.rollbackStarted:
		t.Error("rollback entered while deploy was draining stale releases")
	case <-time.After(40 * time.Millisecond):
	}
	close(ro.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-rbDone; err != nil {
		t.Fatal(err)
	}
	live, err := srv.store.ActiveDeployment(context.Background(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	ro.mu.Lock()
	defer ro.mu.Unlock()
	if ro.stopped[live.ReleaseID] {
		t.Fatalf("both calls succeeded but active release %s was stopped", live.ReleaseID)
	}
}

func TestDeployBuildSerializesSameAppUpdates(t *testing.T) {
	building, finish := make(chan struct{}), make(chan struct{})
	fe := &fakeExec{build: func(ctx context.Context, _ *kadranv1.ImageBuildRequest, _ execclient.BuildSink) (string, error) {
		close(building)
		select {
		case <-finish:
			return "sha256:test", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}}
	srv, _ := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())
	spec := testSpec()
	spec.AppId = "shop"
	mustCreateApp(t, srv, spec)
	deployed := make(chan error, 1)
	go func() {
		deployed <- srv.Deploy(&kadranv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, newDeployStream(context.Background()))
	}()
	<-building
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	branch := "updated"
	_, err := srv.UpdateApp(ctx, &kadranv1.UpdateAppRequest{AppId: "blog", GitBranch: &branch})
	if err == nil {
		t.Error("same-app update entered while build still owned the app")
	}
	// The unrelated app does not share the queue.
	if _, err := srv.UpdateApp(context.Background(), &kadranv1.UpdateAppRequest{AppId: "shop", GitBranch: &branch}); err != nil {
		t.Fatal(err)
	}
	close(finish)
	if err := <-deployed; err != nil {
		t.Fatal(err)
	}
	app, err := srv.store.GetApp(context.Background(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	if app.GitBranch != "main" {
		t.Errorf("canceled queued update changed the app: %s", app.GitBranch)
	}
}

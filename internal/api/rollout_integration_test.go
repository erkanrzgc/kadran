package api

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/deploy"
	"github.com/erkanrzgc/kadran/internal/execclient"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

// runtimeWorld stands in only for Docker and the external reverse proxy.
// The real API, rollout, readiness gate, drain, reconciler and SQLite run.
type runtimeWorld struct {
	mu             sync.Mutex
	replicas       []execclient.Replica
	live           *proxydrv.Config
	loads          int
	created        int
	secondTraffic  chan struct{}
	failSecondLoad bool
}

func (w *runtimeWorld) EnsureNetwork(context.Context, string) (string, error) { return "test", nil }
func (w *runtimeWorld) CreateReplica(_ context.Context, opts execclient.CreateReplicaOptions) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.created++
	w.replicas = append(w.replicas, execclient.Replica{AppID: opts.AppID, ReleaseID: opts.ReleaseID, Index: opts.Index, State: kadranv1.ContainerState_CONTAINER_STATE_EXITED})
	return nil
}
func (w *runtimeWorld) StartReplica(_ context.Context, app, rel string, index uint32) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.replicas {
		r := &w.replicas[i]
		if r.AppID == app && r.ReleaseID == rel && r.Index == index {
			r.State = kadranv1.ContainerState_CONTAINER_STATE_RUNNING
			var n int
			_, _ = fmt.Sscanf(rel, "r%d", &n)
			r.IPAddress = fmt.Sprintf("172.18.0.%d", 10+n)
		}
	}
	return nil
}
func (w *runtimeWorld) ListReplicas(_ context.Context, app string) ([]execclient.Replica, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var out []execclient.Replica
	for _, r := range w.replicas {
		if r.AppID == app {
			out = append(out, r)
		}
	}
	return out, nil
}
func (w *runtimeWorld) stop(app, rel string, index *uint32) (uint32, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var count uint32
	for i := range w.replicas {
		r := &w.replicas[i]
		if r.AppID == app && r.ReleaseID == rel && (index == nil || r.Index == *index) {
			r.State = kadranv1.ContainerState_CONTAINER_STATE_EXITED
			r.IPAddress = ""
			count++
		}
	}
	return count, nil
}
func (w *runtimeWorld) StopRelease(_ context.Context, app, rel string, _ time.Duration) (uint32, error) {
	return w.stop(app, rel, nil)
}
func (w *runtimeWorld) StopReplica(_ context.Context, app, rel string, index uint32, _ time.Duration) (uint32, error) {
	return w.stop(app, rel, &index)
}
func (w *runtimeWorld) Load(_ context.Context, cfg *proxydrv.Config) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.loads++
	if w.loads == 2 && w.failSecondLoad {
		return errors.New("proxy load rejected")
	}
	w.live = cfg
	if w.loads == 2 && w.secondTraffic != nil {
		close(w.secondTraffic)
	}
	return nil
}
func (w *runtimeWorld) Current(context.Context) (*proxydrv.Config, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.live, nil
}

func realRolloutServer(t *testing.T, world *runtimeWorld) *Server {
	t.Helper()
	srv, db := newDeployServer(t, okBuild())
	rec, err := deploy.New(db, world, world, proxydrv.Admin{Listen: "fd/3", Origins: []string{"localhost"}})
	if err != nil {
		t.Fatal(err)
	}
	ro, err := deploy.NewRollout(world, db, rec, deploy.NewHTTPProber(time.Second), deploy.GateOptions{Successes: 1, Interval: time.Millisecond, Timeout: time.Second}, deploy.DrainOptions{Window: 60 * time.Millisecond, Grace: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	srv.rollout = ro
	srv.reconciler = rec
	spec := testSpec()
	spec.Domain = "blog.example.com"
	spec.HealthPath = ""
	mustCreateApp(t, srv, spec)
	return srv
}

func TestRealRolloutRollbackDuringDrainRetainsLiveReplica(t *testing.T) {
	world := &runtimeWorld{secondTraffic: make(chan struct{})}
	srv := realRolloutServer(t, world)
	deployN(t, srv, 1)
	done := make(chan error, 1)
	go func() {
		done <- srv.Deploy(&kadranv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, newDeployStream(context.Background()))
	}()
	<-world.secondTraffic
	resp, err := srv.Rollback(context.Background(), &kadranv1.RollbackRequest{AppId: "blog"})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if resp.GetFromReleaseId() != "r2" || resp.GetToReleaseId() != "r1" {
		t.Fatalf("wrong rollback: %+v", resp)
	}
	live, err := srv.store.ActiveDeployment(context.Background(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	replicas, _ := world.ListReplicas(context.Background(), "blog")
	for _, replica := range replicas {
		if replica.ReleaseID == live.ReleaseID && !replica.Routable() {
			t.Fatalf("successful deploy and rollback stopped active release %s", live.ReleaseID)
		}
		if replica.ReleaseID != live.ReleaseID && replica.Routable() {
			t.Fatalf("stale release %s was not drained", replica.ReleaseID)
		}
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	ups := proxydrv.LiveUpstreams(world.live, "blog.example.com")
	if len(ups) != 1 || ups[0].Dial != "172.18.0.11:8080" {
		t.Fatalf("proxy does not match active release: %v", ups)
	}
	if world.created != 2 || len(world.replicas) != 2 {
		t.Fatal("rollback recreated or removed retained containers")
	}
}

func TestFailedProxyLoadKeepsPreviousRouteAndReplica(t *testing.T) {
	world := &runtimeWorld{failSecondLoad: true}
	srv := realRolloutServer(t, world)
	deployN(t, srv, 1)
	stream := newDeployStream(context.Background())
	if err := srv.Deploy(&kadranv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, stream); err == nil || stream.succeeded() != nil {
		t.Fatal("failed proxy load reported deploy success")
	}
	replicas, _ := world.ListReplicas(context.Background(), "blog")
	if len(replicas) != 2 || !replicas[0].Routable() {
		t.Fatal("failed proxy load stopped previous healthy release")
	}
	world.mu.Lock()
	defer world.mu.Unlock()
	ups := proxydrv.LiveUpstreams(world.live, "blog.example.com")
	if len(ups) != 1 || ups[0].Dial != "172.18.0.11:8080" {
		t.Fatalf("failed load changed previous route: %v", ups)
	}
}

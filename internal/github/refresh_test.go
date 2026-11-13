package github

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const twoRepos = `{"total_count":2,"repositories":[{"full_name":"octo/priv","private":true},{"full_name":"octo/pub","private":false}]}`

func writeBody(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }
}

// gatedRepos, depo listesi isteğini g açılana dek bekleten sahte GitHub kurar.
func gatedRepos(t *testing.T) (*fakeGH, *gate) {
	t.Helper()
	gh := connectedGH()
	g := newGate()
	gh.handle("GET", "/user/installations/7/repositories", g.wrap(writeBody(twoRepos)))
	t.Cleanup(g.open)
	return gh, g
}

func TestFirstDecideAfterRestartUsesWarmCache(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, true, nil)
	e.waitCoverage(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // süresi dolmuş ctx bile önbellekten yanıt alır
	d, err := e.s.Decide(ctx, "octo", "priv")
	if d != DecisionArchive || err != nil {
		t.Fatalf("Decide = %v, %v", d, err)
	}
	if n := gh.count("GET /user/installations?"); n != 1 {
		t.Fatalf("installation list requests = %d, want the single warm-up", n)
	}
}

func TestDecideCallerCancelDoesNotBurnAttempt(t *testing.T) {
	gh, g := gatedRepos(t)
	e := newEnv(t, gh, true, nil)
	g.waitEntered(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if d, err := e.s.Decide(ctx, "octo", "priv"); !errors.Is(err, context.Canceled) || d == DecisionArchive {
		t.Fatalf("Decide = %v, %v; want a pre-decision ctx error", d, err)
	}
	g.open()
	e.waitCoverage(t)
	if d, err := e.s.Decide(context.Background(), "octo", "priv"); d != DecisionArchive || err != nil {
		t.Fatalf("Decide after the refresh = %v, %v", d, err)
	}
	if n := gh.count("GET /user/installations?"); n != 1 {
		t.Fatalf("installation list requests = %d, want 1 (the cancel must not trigger another)", n)
	}
}

func TestStatusDoesNotBlockOnRefresh(t *testing.T) {
	gh, g := gatedRepos(t)
	e := newEnv(t, gh, true, nil)
	g.waitEntered(t)
	res := make(chan *kadranv1.GitHubConnection, 1)
	go func() {
		c, _ := e.s.Status(context.Background())
		res <- c
	}()
	select {
	case c := <-res:
		if c.State != kadranv1.GitHubConnection_STATE_CONNECTED || len(c.Installations) != 0 {
			t.Fatalf("status = %v", c)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Status blocked on the refresh")
	}
	g.open()
	e.waitCoverage(t)
	if c := e.status(t); len(c.Installations) != 1 {
		t.Fatalf("status = %v", c)
	}
}

func TestDisconnectDuringRefreshDiscardsResult(t *testing.T) {
	gh, g := gatedRepos(t)
	e := newEnv(t, gh, true, nil)
	g.waitEntered(t)
	if err := e.s.Disconnect(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.open()
	e.s.covMu.Lock()
	leaked := e.s.cov.loaded || len(e.s.cov.installs) != 0 || len(e.s.cov.index) != 0 || e.s.refresh != nil
	e.s.covMu.Unlock()
	if leaked {
		t.Fatal("refresh result survived Disconnect")
	}
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_NOT_CONNECTED || len(c.Installations) != 0 {
		t.Fatalf("status = %v", c)
	}
}

func TestStaleGenerationResultIsDiscarded(t *testing.T) {
	e := newEnv(t, connectedGH(), true, nil)
	e.waitCoverage(t)
	e.s.covMu.Lock()
	old := e.s.gen
	e.s.gen++
	e.s.cov = coverage{}
	e.s.covMu.Unlock()
	e.s.commitRefresh(nil, old, []*kadranv1.GitHubInstallation{{Account: "x"}}, map[string]bool{"a/b": true}, nil)
	e.s.covMu.Lock()
	defer e.s.covMu.Unlock()
	if e.s.cov.loaded || e.s.cov.attempted || len(e.s.cov.index) != 0 {
		t.Fatalf("stale result installed: %+v", e.s.cov)
	}
}

func TestDecideNetworkErrorIsPreDecision(t *testing.T) {
	gh := connectedGH()
	gh.handle("GET", "/user/installations", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	e := newEnv(t, gh, true, nil)
	d, err := e.s.Decide(context.Background(), "octo", "priv")
	if err == nil || d == DecisionArchive {
		t.Fatalf("Decide = %v, %v; want an error and no archive", d, err)
	}
}

func TestDisconnectDuringInstallDoesNotResurrect(t *testing.T) {
	e := newEnv(t, flowGH(tokenBody), false, nil)
	inSave, release := make(chan struct{}, 1), make(chan struct{})
	old := saveStateFn
	saveStateFn = func(p string, s state) error {
		select {
		case inSave <- struct{}{}:
		default:
		}
		<-release
		return old(p, s)
	}
	t.Cleanup(func() { saveStateFn = old })
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-inSave:
	case <-time.After(10 * time.Second):
		t.Fatal("flow never reached the install step")
	}
	disc := make(chan error, 1)
	go func() { disc <- e.s.Disconnect(context.Background()) }()
	close(release) // kurulum ile Disconnect hangi sırada biterse bitsin sonuç aynı olmalı
	select {
	case err := <-disc:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Disconnect did not return")
	}
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_NOT_CONNECTED || c.Login != "" || len(c.Installations) != 0 {
		t.Fatalf("status = %v", c)
	}
	if _, ok, _ := loadState(e.path, testClientID); ok {
		t.Fatal("state file resurrected after Disconnect")
	}
	if d, _ := e.s.Decide(context.Background(), "octo", "priv"); d != DecisionNotConnected {
		t.Fatalf("Decide = %v", d)
	}
}

func TestPrivateTarballNotFoundMarksCacheStale(t *testing.T) {
	gh := connectedGH()
	var removed atomic.Bool
	gh.handle("GET", "/user/installations/7/repositories", func(w http.ResponseWriter, _ *http.Request) {
		if removed.Load() {
			_, _ = w.Write([]byte(`{"total_count":1,"repositories":[{"full_name":"octo/pub","private":false}]}`))
			return
		}
		_, _ = w.Write([]byte(twoRepos))
	})
	gh.handle("GET", "/repos/octo/priv/tarball/"+testSHA, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
	e := newEnv(t, gh, true, nil)
	e.waitCoverage(t)
	e.clock.Advance(2 * time.Minute)
	if _, err := e.s.Tarball(context.Background(), "octo", "priv", testSHA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Tarball err = %v", err)
	}
	removed.Store(true)
	if d, err := e.s.Decide(context.Background(), "octo", "priv"); d != DecisionNotCovered || err != nil {
		t.Fatalf("Decide = %v, %v; the stale entry must be refreshed away", d, err)
	}
}

func TestVerificationURIMustBeGitHub(t *testing.T) {
	for _, uri := range []string{
		"https://github.com.evil.example/login/device", "https://evil.example/login/device",
		"http://github.com/login/device", "https://github.com:8443/login/device",
		"https://user@github.com/login/device", "https://github.com/login/device?x=1", "https://github.com/login/device#f",
	} {
		gh := flowGH(tokenBody)
		gh.body("POST", "/login/device/code", strings.Replace(codeBody, "https://github.com/login/device", uri, 1))
		e := newEnv(t, gh, false, nil)
		if _, err := e.s.StartDeviceFlow(context.Background()); err == nil {
			t.Errorf("%s accepted", uri)
			continue
		}
		if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL {
			t.Errorf("%s: status = %v", uri, c)
		}
	}
}

func TestCoverageSkipsInvalidNames(t *testing.T) {
	gh := connectedGH()
	gh.body("GET", "/user/installations", `{"total_count":2,"installations":[`+
		`{"id":7,"account":{"login":"octo"},"repository_selection":"selected"},`+
		`{"id":8,"account":{"login":"bad_login"},"repository_selection":"all"}]}`)
	gh.body("GET", "/user/installations/7/repositories", `{"total_count":5,"repositories":[`+
		`{"full_name":"octo/ok","private":true},{"full_name":"octo/..","private":true},`+
		`{"full_name":"a/b/c","private":true},{"full_name":"noslash","private":true},{"full_name":"octo/sp ace","private":true}]}`)
	e := newEnv(t, gh, true, nil)
	e.waitCoverage(t)
	c := e.status(t)
	if len(c.Installations) != 1 || len(c.Installations[0].Repositories) != 1 || c.Installations[0].Repositories[0].FullName != "octo/ok" {
		t.Fatalf("installations = %v", c.Installations)
	}
	if d, _ := e.s.Decide(context.Background(), "octo", "ok"); d != DecisionArchive {
		t.Fatalf("Decide = %v", d)
	}
}

func TestInvalidConnectingLoginFailsFlow(t *testing.T) {
	gh := flowGH(tokenBody)
	gh.body("GET", "/user", `{"login":"bad_login","id":1}`)
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL {
		t.Fatalf("status = %v", c)
	}
}

func TestStatusHidesInvalidLoginFromState(t *testing.T) {
	gh := connectedGH()
	srvEnv := newEnv(t, gh, false, nil)
	st := sampleState()
	st.Login = "bad_login"
	if err := srvEnv.s.tk.install(st); err != nil {
		t.Fatal(err)
	}
	if c := srvEnv.status(t); c.Login != "" || c.State != kadranv1.GitHubConnection_STATE_CONNECTED {
		t.Fatalf("status = %v", c)
	}
}

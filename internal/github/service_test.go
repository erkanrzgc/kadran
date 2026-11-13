package github

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

func TestDisconnectRemovesStateAndCancelsFlow(t *testing.T) {
	entered := make(chan struct{}, 1)
	e := newEnv(t, connectedGH(), true, blockingSleep(entered))
	ctx := context.Background()
	if _, err := e.s.Decide(ctx, "octo", "priv"); err != nil { // önbelleği doldur
		t.Fatal(err)
	}
	if _, err := e.s.StartDeviceFlow(ctx); err != nil {
		t.Fatal(err)
	}
	<-entered
	e.s.lifeMu.Lock()
	fr := e.s.flow
	e.s.lifeMu.Unlock()

	if err := e.s.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fr.done:
	default:
		t.Fatal("flow goroutine still running after Disconnect")
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatalf("state file must be removed: %v", err)
	}
	c := e.status(t)
	if c.State != kadranv1.GitHubConnection_STATE_NOT_CONNECTED || c.Login != "" || c.Pending != nil || len(c.Installations) != 0 || c.InstallUrl == "" {
		t.Fatalf("status = %v", c)
	}
	if d, err := e.s.Decide(ctx, "octo", "priv"); d != DecisionNotConnected || err != nil {
		t.Fatalf("Decide = %v, %v", d, err)
	}
	if err := e.s.Disconnect(ctx); err != nil { // tekrar çağrı zararsız
		t.Fatal(err)
	}
}

func TestCloseIsIdempotentAndStopsStart(t *testing.T) {
	entered := make(chan struct{}, 1)
	e := newEnv(t, flowGH(pending), false, blockingSleep(entered))
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	e.s.lifeMu.Lock()
	fr := e.s.flow
	e.s.lifeMu.Unlock()
	e.s.Close()
	e.s.Close()
	select {
	case <-fr.done:
	default:
		t.Fatal("Close must wait for the polling goroutine")
	}
	if _, err := e.s.StartDeviceFlow(context.Background()); err == nil {
		t.Fatal("Start after Close must fail")
	}
}

func TestStatusConnectedFromStateFile(t *testing.T) {
	e := newEnv(t, connectedGH(), true, nil)
	e.waitCoverage(t)
	c := e.status(t)
	if c.State != kadranv1.GitHubConnection_STATE_CONNECTED || c.Login != "octo" || c.ReconnectRequired ||
		len(c.Installations) != 1 || !c.ConnectedAt.AsTime().Equal(tokenTestNow) {
		t.Fatalf("status = %v", c)
	}
}

func TestStatusRefreshesStaleCoverageOnlyAfterAMinute(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, true, nil)
	e.waitCoverage(t)
	e.status(t)
	e.status(t)
	e.waitCoverage(t)
	if n := gh.count("GET /user/installations?"); n != 1 {
		t.Fatalf("requests = %d, want 1 (the warm-up only)", n)
	}
	e.clock.Advance(61e9)
	e.status(t)
	e.waitCoverage(t)
	if n := gh.count("GET /user/installations?"); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "state.json")
	good := Config{ClientID: testClientID, AppSlug: "kadran-deploy", StateFile: abs}
	if s, err := New(good); err != nil {
		t.Fatal(err)
	} else {
		s.Close()
	}
	for name, mut := range map[string]func(*Config){
		"empty state":    func(c *Config) { c.StateFile = "" },
		"relative state": func(c *Config) { c.StateFile = "state.json" },
		"bad client id":  func(c *Config) { c.ClientID = "a b" },
		"upper slug":     func(c *Config) { c.AppSlug = "Kadran" },
		"slash slug":     func(c *Config) { c.AppSlug = "a/b" },
		"long slug":      func(c *Config) { c.AppSlug = strings.Repeat("a", 101) },
	} {
		c := good
		mut(&c)
		if s, err := New(c); err == nil {
			s.Close()
			t.Errorf("%s: New accepted the config", name)
		}
	}
}

func TestNewAppliesDefaultsForEmptyIdentity(t *testing.T) {
	s, err := New(Config{StateFile: filepath.Join(t.TempDir(), "state.json")})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.clientID != DefaultClientID || s.appSlug != DefaultAppSlug {
		t.Fatalf("identity = %q, %q", s.clientID, s.appSlug)
	}
}

func TestDefaultsAreTheDocumentedConstants(t *testing.T) {
	if DefaultClientID != "Iv23lizBm3zlCdYH3NQ5" || DefaultAppSlug != "kadran-deploy" {
		t.Fatal("default app identity changed")
	}
}

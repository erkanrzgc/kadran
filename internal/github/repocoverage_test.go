package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

func TestSuccessFetchesLoginAndInstallations(t *testing.T) {
	gh := flowGH(tokenBody)
	// 101 depo: ikinci sayfa gerekir.
	gh.handle("GET", "/user/installations/7/repositories", func(w http.ResponseWriter, r *http.Request) {
		var parts []string
		if r.URL.Query().Get("page") == "2" {
			parts = append(parts, `{"full_name":"octo/last","private":true}`)
		} else {
			for i := 0; i < 100; i++ {
				parts = append(parts, fmt.Sprintf(`{"full_name":"octo/r%d","private":false}`, i))
			}
		}
		_, _ = w.Write([]byte(`{"total_count":101,"repositories":[` + strings.Join(parts, ",") + `]}`))
	})
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	c := e.status(t)
	if c.Login != "octo" || len(c.Installations) != 1 {
		t.Fatalf("status = %v", c)
	}
	in := c.Installations[0]
	if in.Account != "octo" || in.AllRepositories || in.Truncated || len(in.Repositories) != 101 {
		t.Fatalf("installation = %v (%d repos)", in.Account, len(in.Repositories))
	}
	var sawPerPage, sawPage2 bool
	for _, r := range gh.requests() {
		sawPerPage = sawPerPage || strings.Contains(r, "/user/installations/7/repositories?") && strings.Contains(r, "per_page=100")
		sawPage2 = sawPage2 || strings.Contains(r, "/repositories?") && strings.Contains(r, "page=2")
	}
	if !sawPerPage || !sawPage2 {
		t.Fatalf("requests = %v", gh.requests())
	}
	for _, a := range gh.authHeaders("GET", "/user/installations") {
		if a != "Bearer ghu_new" {
			t.Fatalf("Authorization = %q", a)
		}
	}
}

func TestInstallationListTruncatesAt1000(t *testing.T) {
	gh := flowGH(tokenBody)
	gh.handle("GET", "/user/installations/7/repositories", func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		var parts []string
		for i := 0; i < 100; i++ {
			parts = append(parts, fmt.Sprintf(`{"full_name":"octo/p%s-%d","private":true}`, page, i))
		}
		_, _ = w.Write([]byte(`{"total_count":1500,"repositories":[` + strings.Join(parts, ",") + `]}`))
	})
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	in := e.status(t).Installations[0]
	if !in.Truncated || len(in.Repositories) != 1000 {
		t.Fatalf("truncated %v, repos %d; want true, 1000", in.Truncated, len(in.Repositories))
	}
	if n := gh.count("GET /user/installations/7/repositories"); n != 10 {
		t.Fatalf("repository page requests = %d, want 10", n)
	}
}

func TestStatusWithoutInstallationShowsInstallURL(t *testing.T) {
	gh := flowGH(tokenBody)
	gh.body("GET", "/user/installations", `{"total_count":0,"installations":[]}`)
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	c := e.status(t)
	if c.State != kadranv1.GitHubConnection_STATE_CONNECTED || len(c.Installations) != 0 ||
		c.InstallUrl != "https://github.com/apps/kadran-deploy/installations/new" {
		t.Fatalf("status = %v", c)
	}
}

func TestCoverageFailureDoesNotFailConnection(t *testing.T) {
	gh := flowGH(tokenBody)
	gh.handle("GET", "/user/installations", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) })
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_CONNECTED || c.Login != "octo" {
		t.Fatalf("status = %v", c)
	}
}

func TestArchiveDecisionUsesPrivateFlag(t *testing.T) {
	gh := connectedGH()
	gh.body("GET", "/repos/octo/pub/tarball/"+testSHA, "SHOULD-NOT-BE-FETCHED")
	e := newEnv(t, gh, true, nil)
	ctx := context.Background()

	d, rc, err := e.s.Archive(ctx, "octo", "pub", testSHA)
	if d != DecisionNotCovered || rc != nil || err != nil {
		t.Fatalf("public = %v, %v, %v", d, rc, err)
	}
	if gh.count("GET /repos/octo/pub/tarball") != 0 {
		t.Fatal("tarball requested for a public repository")
	}
	// Eşleşme büyük/küçük harfe duyarsız.
	gh.body("GET", "/repos/OCTO/Priv/tarball/"+testSHA, "TARDATA")
	d, rc, err = e.s.Archive(ctx, "OCTO", "Priv", testSHA)
	if d != DecisionArchive || err != nil || rc == nil {
		t.Fatalf("private = %v, %v, %v", d, rc, err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "TARDATA" {
		t.Fatalf("body = %q", body)
	}
}

func TestDecisionZeroValueIsNotArchive(t *testing.T) {
	var d Decision
	if d == DecisionArchive || d != DecisionNotConnected {
		t.Fatalf("zero Decision = %v", d)
	}
}

func TestArchiveNotListedRefreshesAtMostOncePerMinute(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, true, nil)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if d, err := e.s.Decide(ctx, "octo", "unknown"); d != DecisionNotCovered || err != nil {
			t.Fatalf("Decide = %v, %v", d, err)
		}
	}
	if n := gh.count("GET /user/installations?"); n != 1 {
		t.Fatalf("installation list requests = %d, want 1", n)
	}
	e.clock.Advance(59 * time.Second)
	_, _ = e.s.Decide(ctx, "octo", "unknown")
	if n := gh.count("GET /user/installations?"); n != 1 {
		t.Fatalf("refreshed inside the minute: %d", n)
	}
	e.clock.Advance(2 * time.Second)
	_, _ = e.s.Decide(ctx, "octo", "unknown")
	if n := gh.count("GET /user/installations?"); n != 2 {
		t.Fatalf("installation list requests = %d, want 2 after a minute", n)
	}
}

func TestNewlyInstalledRepoIsFoundByRefresh(t *testing.T) {
	gh := connectedGH()
	var added atomic.Bool
	gh.handle("GET", "/user/installations/7/repositories", func(w http.ResponseWriter, _ *http.Request) {
		extra := ""
		if added.Load() {
			extra = `,{"full_name":"octo/new","private":true}`
		}
		_, _ = w.Write([]byte(`{"total_count":2,"repositories":[{"full_name":"octo/priv","private":true}` + extra + `]}`))
	})
	e := newEnv(t, gh, true, nil)
	ctx := context.Background()
	_, _ = e.s.Decide(ctx, "octo", "priv")
	if d, _ := e.s.Decide(ctx, "octo", "new"); d != DecisionNotCovered {
		t.Fatalf("Decide = %v before install", d)
	}
	added.Store(true)
	e.clock.Advance(61 * time.Second)
	if d, err := e.s.Decide(ctx, "octo", "new"); d != DecisionArchive || err != nil {
		t.Fatalf("Decide = %v, %v after refresh", d, err)
	}
}

func TestArchiveNotConnected(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, false, nil)
	d, rc, err := e.s.Archive(context.Background(), "octo", "priv", testSHA)
	if d != DecisionNotConnected || rc != nil || err != nil {
		t.Fatalf("Archive = %v, %v, %v", d, rc, err)
	}
	if n := len(gh.requests()); n != 0 {
		t.Fatalf("%d requests while not connected", n)
	}
}

func TestDecideRejectsBadRefBeforeAnyRequest(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, true, nil)
	e.waitCoverage(t)
	before := len(gh.requests())
	d, err := e.s.Decide(context.Background(), "octo", "../x")
	if err == nil || d == DecisionArchive || len(gh.requests()) != before {
		t.Fatalf("Decide = %v, %v, %d new requests", d, err, len(gh.requests())-before)
	}
}

func TestPrivateArchiveErrorIsNotFallback(t *testing.T) {
	gh := connectedGH()
	gh.handle("GET", "/repos/octo/priv/tarball/"+testSHA, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	e := newEnv(t, gh, true, nil)
	d, rc, err := e.s.Archive(context.Background(), "octo", "priv", testSHA)
	if d != DecisionArchive || rc != nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("Archive = %v, %v, %v; want DecisionArchive + ErrNotFound", d, rc, err)
	}
}

// refreshGH ilk tarball isteğinde eski jetonu reddeden, yenilemeyi sayan sahte GitHub'dır.
func refreshGH(secondAlso401 bool, refreshes *atomic.Int32) *fakeGH {
	gh := connectedGH()
	gh.handle("POST", "/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		refreshes.Add(1)
		_, _ = w.Write([]byte(tokenBody))
	})
	gh.handle("GET", "/repos/octo/priv/tarball/"+testSHA, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer ghu_new" && !secondAlso401 {
			_, _ = w.Write([]byte("TARDATA"))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	})
	return gh
}

func TestUnauthorizedForcesOneRefresh(t *testing.T) {
	var refreshes atomic.Int32
	e := newEnv(t, refreshGH(false, &refreshes), true, nil)
	rc, err := e.s.Tarball(context.Background(), "octo", "priv", testSHA)
	if err != nil {
		t.Fatal(err)
	}
	_ = rc.Close()
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes = %d, want 1", refreshes.Load())
	}
	if st, ok, _ := loadState(e.path, testClientID); !ok || st.AccessToken != "ghu_new" {
		t.Fatalf("refreshed pair not persisted: %+v", st)
	}
}

func TestSecondUnauthorizedRequiresReconnect(t *testing.T) {
	var refreshes atomic.Int32
	e := newEnv(t, refreshGH(true, &refreshes), true, nil)
	if _, err := e.s.Tarball(context.Background(), "octo", "priv", testSHA); !errors.Is(err, ErrReconnect) {
		t.Fatalf("err = %v, want ErrReconnect", err)
	}
	if refreshes.Load() != 1 {
		t.Fatalf("refreshes = %d, want exactly 1", refreshes.Load())
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatalf("state file must be removed: %v", err)
	}
	if c := e.status(t); !c.ReconnectRequired || c.State == kadranv1.GitHubConnection_STATE_CONNECTED {
		t.Fatalf("status = %v", c)
	}
	if d, err := e.s.Decide(context.Background(), "octo", "priv"); d != DecisionNotConnected || err != nil {
		t.Fatalf("Decide after drop = %v, %v", d, err)
	}
}

func TestTarballRejectsBadSHAWithoutRequest(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, true, nil)
	e.waitCoverage(t)
	before := len(gh.requests())
	if _, err := e.s.Tarball(context.Background(), "octo", "priv", "abc"); err == nil {
		t.Fatal("bad sha accepted")
	}
	if len(gh.requests()) != before {
		t.Fatal("request sent for an invalid sha")
	}
}

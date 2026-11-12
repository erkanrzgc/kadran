package github

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestResolveBranchAcceptsSlashes(t *testing.T) {
	gh := connectedGH()
	var accept, rawPath string
	gh.handle("GET", "/repos/octo/priv/commits/feature/x", func(w http.ResponseWriter, r *http.Request) {
		accept, rawPath = r.Header.Get("Accept"), r.URL.EscapedPath()
		_, _ = w.Write([]byte(testSHA + "\n"))
	})
	e := newEnv(t, gh, true, nil)
	sha, err := e.s.ResolveBranch(context.Background(), "octo", "priv", "feature/x")
	if err != nil || sha != testSHA {
		t.Fatalf("ResolveBranch = %q, %v", sha, err)
	}
	if accept != "application/vnd.github.sha" || rawPath != "/repos/octo/priv/commits/feature/x" {
		t.Fatalf("accept %q path %q", accept, rawPath)
	}
}

func TestResolveBranchEscapesSegments(t *testing.T) {
	gh := connectedGH()
	var raw string
	gh.handle("GET", "/repos/octo/priv/commits/a#b/c%d", func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.EscapedPath()
		_, _ = w.Write([]byte(testSHA))
	})
	e := newEnv(t, gh, true, nil)
	if _, err := e.s.ResolveBranch(context.Background(), "octo", "priv", "a#b/c%d"); err != nil {
		t.Fatal(err)
	}
	if raw != "/repos/octo/priv/commits/a%23b/c%25d" {
		t.Fatalf("escaped path = %q", raw)
	}
}

func TestResolveBranchRejectsUnsafeNames(t *testing.T) {
	gh := connectedGH()
	e := newEnv(t, gh, true, nil)
	bad := []string{"", "..", "a..b", "a//b", "-x", "/x", "x/", "x.lock", "a/x.lock", ".hidden", "a/.b", "x.", "@", "a@{b",
		strings.Repeat("a", 256), "a b", "a:b", "a~b", "a^b", "a?b", "a*b", "a[b", `a\b`, "a\x00b", "a\tb", "a\x7fb", "a\xffb"}
	for _, name := range bad {
		if err := validBranch(name); err == nil {
			t.Errorf("validBranch(%q) accepted", name)
		}
		if _, err := e.s.ResolveBranch(context.Background(), "octo", "priv", name); err == nil {
			t.Errorf("ResolveBranch(%q) accepted", name)
		}
	}
	if n := len(gh.requests()); n != 0 {
		t.Fatalf("%d requests for rejected names", n)
	}
	for _, ok := range []string{"main", "feature/x", "release-1.2", "a/b/c", "v1.0.0", strings.Repeat("a", 255), "ünï"} {
		if err := validBranch(ok); err != nil {
			t.Errorf("validBranch(%q) = %v", ok, err)
		}
	}
}

func TestResolveBranchRejectsBadRepoRef(t *testing.T) {
	e := newEnv(t, connectedGH(), true, nil)
	if _, err := e.s.ResolveBranch(context.Background(), "oc/to", "priv", "main"); err == nil {
		t.Fatal("bad owner accepted")
	}
}

func TestResolveBranchRequiresFullSHA(t *testing.T) {
	gh := connectedGH()
	gh.body("GET", "/repos/octo/priv/commits/main", "abc")
	e := newEnv(t, gh, true, nil)
	if sha, err := e.s.ResolveBranch(context.Background(), "octo", "priv", "main"); err == nil {
		t.Fatalf("short sha accepted: %q", sha)
	}
}

func TestResolveBranchRefreshesOnceOn401(t *testing.T) {
	gh := connectedGH()
	gh.body("POST", "/login/oauth/access_token", tokenBody)
	gh.handle("GET", "/repos/octo/priv/commits/main", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghu_new" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(testSHA))
	})
	e := newEnv(t, gh, true, nil)
	if sha, err := e.s.ResolveBranch(context.Background(), "octo", "priv", "main"); err != nil || sha != testSHA {
		t.Fatalf("ResolveBranch = %q, %v", sha, err)
	}
	if gh.count("POST /login/oauth/access_token") != 1 {
		t.Fatal("expected exactly one refresh")
	}
}

func TestResolveBranchNotConnected(t *testing.T) {
	e := newEnv(t, connectedGH(), false, nil)
	if _, err := e.s.ResolveBranch(context.Background(), "octo", "priv", "main"); err == nil {
		t.Fatal("expected an error when not connected")
	}
}

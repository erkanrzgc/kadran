package github

import (
	"context"
	"errors"
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
	e.waitCoverage(t)
	before := len(gh.requests())
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
	if n := len(gh.requests()) - before; n != 0 {
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

// Gerçek GitHub olmayan bir dal için commits uç noktasında 404 değil 422
// döner (17 Kas ölçümü). Genel hata olarak kalırsa kadran-git onu genel
// başarısızlık koduyla döner ve kadrand "kadran-git çalışmıyor" derdi.
func TestResolveBranchMissingBranchIsNotFound(t *testing.T) {
	gh := connectedGH()
	gh.handle("GET", "/repos/octo/priv/commits/master", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"No commit found for SHA: master"}`))
	})
	e := newEnv(t, gh, true, nil)
	sha, err := e.s.ResolveBranch(context.Background(), "octo", "priv", "master")
	if !errors.Is(err, ErrNotFound) || sha != "" {
		t.Fatalf("ResolveBranch = %q, %v; want ErrNotFound", sha, err)
	}
	if strings.Contains(err.Error(), "No commit") {
		t.Fatalf("error carries the response body: %v", err)
	}
}

// 422 yalnız ResolveBranch'te "bulunamadı"dır: başka çağrılarda genel hata
// kalır (ör. arşiv ya da kurulum listesi 422 alırsa depo yok sayılmaz).
func TestUnprocessableIsNotFoundOnlyForBranches(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	})
	err := c.getJSON(context.Background(), "/x", "t", nil)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("getJSON 422 = %v, want a generic error that is not ErrNotFound", err)
	}
	if _, err := c.getRaw(context.Background(), "/x", "t", "application/vnd.github.sha", maxSHABody); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("getRaw 422 = %v, want a generic error that is not ErrNotFound", err)
	}
}

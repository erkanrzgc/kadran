package exec

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/audit"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	dArchive      = kadranv1.FetchArchiveResponse_DECISION_ARCHIVE
	dNotConnected = kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED
	dNotCovered   = kadranv1.FetchArchiveResponse_DECISION_NOT_COVERED
	publicHint    = "if it is private, install the GitHub app on it: see kadran github status"
)

func TestDecisionTimeoutDefaultIsFiveSeconds(t *testing.T) {
	g, ok := NewGitFetcher("/x").(*gitFetcher)
	if !ok || g.decisionTimeout != 5*time.Second || gitDecisionTimeout != 5*time.Second {
		t.Fatalf("decision timeout is not 5s: %+v", g)
	}
	if g.chunkIdle != gitChunkIdleTimeout || gitChunkIdleTimeout != 60*time.Second {
		t.Fatalf("chunk idle default is not 60s: %+v", g)
	}
	if DefaultGitFetchSocket != "/run/kadran-git/fetch.sock" {
		t.Fatal("wrong default socket")
	}
}

func lastRecord(t *testing.T, srv *Server) audit.Record {
	t.Helper()
	recs, err := srv.journal.Read(0, 100)
	if err != nil || len(recs) == 0 {
		t.Fatalf("no audit records: %v", err)
	}
	return recs[len(recs)-1]
}

func paramOf(t *testing.T, raw, key string) string {
	t.Helper()
	m := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("params not JSON: %v", err)
	}
	return m[key]
}

// stubFetcher, soketsiz (Windows'ta da koşan) sahte GitFetcher'dır.
type stubFetcher struct {
	calls    atomic.Int32
	decision kadranv1.FetchArchiveResponse_Decision
	file     *os.File
	err      error
}

func (s *stubFetcher) Fetch(context.Context, *kadranv1.GitSource) (kadranv1.FetchArchiveResponse_Decision, *os.File, error) {
	s.calls.Add(1)
	return s.decision, s.file, s.err
}

func TestAllowRepoStillIntersects(t *testing.T) {
	srv := newTestServer(t)
	cf := &stubFetcher{decision: dNotConnected}
	srv.gitFetch = cf
	srv.allowedRepos = []string{"someone/else"}
	err := srv.ImageBuild(validBuildRequest(), newBuildStream())
	if status.Code(err) != codes.InvalidArgument || cf.calls.Load() != 0 {
		t.Fatalf("allowlist bypassed: %v, fetch calls=%d", err, cf.calls.Load())
	}
}

func TestFetchOnlyAskedForDefaultHost(t *testing.T) {
	srv := newTestServer(t)
	cf := &stubFetcher{decision: dNotConnected}
	srv.gitFetch = cf
	srv.allowedGitHosts = []string{DefaultGitHost, "git.example.com"}
	req := validBuildRequest()
	req.Source.Host = "git.example.com"
	_ = srv.ImageBuild(req, newBuildStream())
	if cf.calls.Load() != 0 {
		t.Fatal("decision asked for a non-GitHub host")
	}
	_ = srv.ImageBuild(validBuildRequest(), newBuildStream())
	if cf.calls.Load() != 1 {
		t.Fatal("decision not asked for github.com")
	}
}

func TestPublicFailureGetsHintWhenConnected(t *testing.T) {
	for _, tc := range []struct {
		name string
		d    kadranv1.FetchArchiveResponse_Decision
		hint bool
	}{
		{"not covered", dNotCovered, true},
		{"not connected", dNotConnected, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t) // ulaşılamaz Docker: derleme başarısız olur
			srv.gitFetch = &stubFetcher{decision: tc.d}
			err := srv.ImageBuild(validBuildRequest(), newBuildStream())
			if err == nil {
				t.Fatal("expected build failure")
			}
			if got := strings.Contains(err.Error(), publicHint); got != tc.hint {
				t.Fatalf("hint present=%v want %v: %v", got, tc.hint, err)
			}
		})
	}
}

func TestContextSourceOnBothPaths(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "kadran-context-*.tar")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		f    GitFetcher
		want string
	}{
		{"no fetcher", nil, "public"},
		{"not connected", &stubFetcher{decision: dNotConnected}, "public"},
		{"archive error", &stubFetcher{decision: dArchive, err: errors.New("private archive download failed")}, "github-app"},
		{"archive file", &stubFetcher{decision: dArchive, file: tmp}, "github-app"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t) // Docker ulaşılamaz: kayıt BAŞARISIZ yazılır
			srv.gitFetch = tc.f
			if err := srv.ImageBuild(validBuildRequest(), newBuildStream()); err == nil {
				t.Fatal("expected failure with unreachable docker")
			}
			rec := lastRecord(t, srv)
			if rec.Outcome != audit.OutcomeFailure || paramOf(t, rec.ParamsJSON, "context_source") != tc.want {
				t.Fatalf("want %s, record %+v", tc.want, rec)
			}
		})
	}
	if _, err := os.Stat(tmp.Name()); err == nil {
		t.Fatal("archive file not removed after the build")
	}
}

// Karardan ÖNCEKİ protokol ihlali (ilk ileti karar değil) NOT_CONNECTED'dır:
// hata dönmez, herkese açık yola düşülür. Soket gerektirmez, Windows'ta da koşar.
func TestPreDecisionProtocolViolationFallsBackToPublic(t *testing.T) {
	g := &gitFetcher{decisionTimeout: time.Second, chunkIdle: time.Second}
	first := &kadranv1.FetchArchiveResponse{Kind: &kadranv1.FetchArchiveResponse_Chunk{Chunk: []byte("x")}}
	d, f, err := g.decide(context.Background(), first, nil, func() {})
	if d != dNotConnected || f != nil || err != nil {
		t.Fatalf("pre-decision failure must be NOT_CONNECTED without error: %v %v %v", d, f, err)
	}
}

// ARCHIVE'dan sonraki hata derlemeyi düşürür; herkese açık yola düşülmez.
// Docker ulaşılamaz olduğundan, düşen bir hata ancak kadran-git'in sabit
// metniyle ayırt edilir (public yola düşülseydi Docker hatası gelirdi).
func TestPrivateArchiveErrorFailsBuildWithItsOwnText(t *testing.T) {
	srv := newTestServer(t)
	srv.gitFetch = &stubFetcher{decision: dArchive, err: errors.New("private archive download failed")}
	err := srv.ImageBuild(validBuildRequest(), newBuildStream())
	if err == nil || !strings.Contains(err.Error(), "private archive download failed") {
		t.Fatalf("private fetch error not surfaced: %v", err)
	}
}

package gitref

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const fallbackSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeResolveRPC yalnız GetApp ve ResolveGitHubBranch cagrilarini cevaplar.
type fakeResolveRPC struct {
	kadranv1.KadranServiceClient
	host       string
	getAppErr  error
	resolveErr error
	sha        string
	resolved   []*kadranv1.ResolveGitHubBranchRequest
}

func (f *fakeResolveRPC) GetApp(context.Context, *kadranv1.GetAppRequest, ...grpc.CallOption) (*kadranv1.GetAppResponse, error) {
	if f.getAppErr != nil {
		return nil, f.getAppErr
	}
	return &kadranv1.GetAppResponse{App: &kadranv1.App{Spec: &kadranv1.AppSpec{
		GitHost: f.host, GitOwner: "o", GitRepo: "r", GitBranch: "main",
	}}}, nil
}

func (f *fakeResolveRPC) ResolveGitHubBranch(_ context.Context, in *kadranv1.ResolveGitHubBranchRequest, _ ...grpc.CallOption) (*kadranv1.ResolveGitHubBranchResponse, error) {
	f.resolved = append(f.resolved, in)
	if f.resolveErr != nil {
		return nil, f.resolveErr
	}
	return &kadranv1.ResolveGitHubBranchResponse{CommitSha: f.sha}, nil
}

// stubAnonymous, anonim dal cozumunu aga cikmadan verilen hataya baglar.
func stubAnonymous(t *testing.T, err error) {
	t.Helper()
	old := resolveBranch
	resolveBranch = func(context.Context, string, string, string, string) (string, error) { return "", err }
	t.Cleanup(func() { resolveBranch = old })
}

func TestResolveCommitFallsBackToServer(t *testing.T) {
	stubAnonymous(t, errors.New("repository query returned 404 Not Found"))
	rpc := &fakeResolveRPC{host: "github.com", sha: fallbackSHA}
	var progress bytes.Buffer

	sha, err := ResolveCommit(t.Context(), rpc, "web", "", "dev", &progress)
	if err != nil {
		t.Fatalf("hata: %v", err)
	}
	if sha != fallbackSHA {
		t.Errorf("sha = %q", sha)
	}
	if len(rpc.resolved) != 1 || rpc.resolved[0].GetAppId() != "web" || rpc.resolved[0].GetBranch() != "dev" {
		t.Errorf("ResolveGitHubBranch istekleri = %v", rpc.resolved)
	}
	if !strings.Contains(progress.String(), "resolved through the GitHub app") {
		t.Errorf("ilerleme satiri yok: %q", progress.String())
	}
}

func TestResolveCommitKeepsAnonymousErrorOnOldServer(t *testing.T) {
	anon := errors.New("repository query returned 404 Not Found")
	stubAnonymous(t, anon)
	rpc := &fakeResolveRPC{host: "github.com", resolveErr: status.Error(codes.Unimplemented, "unknown method")}

	_, err := ResolveCommit(t.Context(), rpc, "web", "", "", nil)
	if !errors.Is(err, anon) || err.Error() != anon.Error() {
		t.Errorf("ilk hata korunmadi: %v", err)
	}
}

func TestResolveCommitFallbackErrorKeepsBoth(t *testing.T) {
	anon := errors.New("repository query returned 404 Not Found")
	stubAnonymous(t, anon)
	rpc := &fakeResolveRPC{host: "github.com",
		resolveErr: status.Error(codes.FailedPrecondition, "GitHub is not connected")}

	_, err := ResolveCommit(t.Context(), rpc, "web", "", "", nil)
	if err == nil || !errors.Is(err, anon) {
		t.Fatalf("anonim hata sarilmadi: %v", err)
	}
	for _, want := range []string{"404 Not Found", "through the GitHub app: GitHub is not connected"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q yok: %v", want, err)
		}
	}
}

func TestResolveCommitRejectsBadServerSHA(t *testing.T) {
	stubAnonymous(t, errors.New("anon failed"))
	rpc := &fakeResolveRPC{host: "github.com", sha: "main"}

	if sha, err := ResolveCommit(t.Context(), rpc, "web", "", "", nil); err == nil || sha != "" {
		t.Errorf("gecersiz sha kabul edildi: %q, %v", sha, err)
	}
}

func TestResolveCommitNoFallbackForNonGitHubHost(t *testing.T) {
	anon := errors.New("anon failed")
	stubAnonymous(t, anon)
	rpc := &fakeResolveRPC{host: "gitlab.com", sha: fallbackSHA}

	_, err := ResolveCommit(t.Context(), rpc, "web", "", "", nil)
	if err == nil || err.Error() != anon.Error() {
		t.Errorf("hata = %v", err)
	}
	if len(rpc.resolved) != 0 {
		t.Error("github.com disi depo icin sunucuya dusuldu")
	}
}

// TestResolveCommitDeployKeyStillNeedsCommit: dagitim anahtari GetApp
// cagrisinda PermissionDenied alir; yedek yola HIC dusulmez.
func TestResolveCommitDeployKeyStillNeedsCommit(t *testing.T) {
	stubAnonymous(t, errors.New("anon"))
	rpc := &fakeResolveRPC{host: "github.com", getAppErr: status.Error(codes.PermissionDenied, "no")}

	_, err := ResolveCommit(t.Context(), rpc, "web", "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "pass the commit with -commit") {
		t.Errorf("hata = %v", err)
	}
	if len(rpc.resolved) != 0 {
		t.Error("dagitim anahtari yedek yola dustu")
	}
}

func TestResolveCommitWithCommitSkipsEverything(t *testing.T) {
	rpc := &fakeResolveRPC{}
	sha, err := ResolveCommit(t.Context(), rpc, "web", fallbackSHA, "", nil)
	if err != nil || sha != fallbackSHA || len(rpc.resolved) != 0 {
		t.Errorf("sha=%q err=%v", sha, err)
	}
}

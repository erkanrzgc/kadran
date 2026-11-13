//go:build linux

package gitclient

import (
	"context"
	"net"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// fakeAdmin, kadran-git yonetim sunucusunun sahtesi.
type fakeAdmin struct {
	kadranv1.UnimplementedGitAdminServiceServer
	err error
	got *kadranv1.ResolveBranchRequest
}

func (f *fakeAdmin) StartDeviceFlow(context.Context, *kadranv1.StartDeviceFlowRequest) (*kadranv1.StartDeviceFlowResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &kadranv1.StartDeviceFlowResponse{Prompt: &kadranv1.GitHubUserCodePrompt{UserCode: "ABCD-1234"}}, nil
}

func (f *fakeAdmin) GetGitStatus(context.Context, *kadranv1.GetGitStatusRequest) (*kadranv1.GetGitStatusResponse, error) {
	return &kadranv1.GetGitStatusResponse{Connection: &kadranv1.GitHubConnection{Login: "x"}}, f.err
}

func (f *fakeAdmin) DisconnectGit(context.Context, *kadranv1.DisconnectGitRequest) (*kadranv1.DisconnectGitResponse, error) {
	return &kadranv1.DisconnectGitResponse{}, f.err
}

func (f *fakeAdmin) ResolveBranch(_ context.Context, r *kadranv1.ResolveBranchRequest) (*kadranv1.ResolveBranchResponse, error) {
	f.got = r
	if f.err != nil {
		return nil, f.err
	}
	return &kadranv1.ResolveBranchResponse{CommitSha: strings.Repeat("a", 40)}, nil
}

// serve, kayitli sunucuyla gercek bir unix soketi acar; register nil ise
// hicbir servis kaydedilmez (eski kadran-git: Unimplemented).
func serve(t *testing.T, f *fakeAdmin) *Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin.sock")
	lis, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	if f != nil {
		kadranv1.RegisterGitAdminServiceServer(gs, f)
	}
	go func() { _ = gs.Serve(lis) }()
	t.Cleanup(gs.Stop)

	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestClientRoundTrip(t *testing.T) {
	f := &fakeAdmin{}
	c := serve(t, f)
	ctx := context.Background()

	if p, err := c.StartDeviceFlow(ctx); err != nil || p.GetUserCode() != "ABCD-1234" {
		t.Fatalf("StartDeviceFlow: %v %v", p, err)
	}
	if s, err := c.Status(ctx); err != nil || s.GetLogin() != "x" {
		t.Fatalf("Status: %v %v", s, err)
	}
	if err := c.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	sha, err := c.ResolveBranch(ctx, "o", "r", "b")
	if err != nil || sha != strings.Repeat("a", 40) {
		t.Fatalf("ResolveBranch: %q %v", sha, err)
	}
	if f.got.GetOwner() != "o" || f.got.GetRepo() != "r" || f.got.GetBranch() != "b" {
		t.Errorf("sunucuya giden = %v", f.got)
	}
}

func TestClientNeverForwardsServerText(t *testing.T) {
	c := serve(t, &fakeAdmin{err: status.Error(codes.NotFound, leak)})
	_, err := c.ResolveBranch(context.Background(), "o", "r", "b")
	if status.Code(err) != codes.NotFound || strings.Contains(err.Error(), "ghu_") {
		t.Fatalf("err = %v", err)
	}
}

func TestMissingSocketIsNotRunning(t *testing.T) {
	c, err := Dial(filepath.Join(t.TempDir(), "yok.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_, err = c.Status(context.Background())
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), MsgNotRunning) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnimplementedKadranGitSaysUpgrade(t *testing.T) {
	c := serve(t, nil)
	_, err := c.Status(context.Background())
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "upgrade the server") {
		t.Fatalf("err = %v", err)
	}
}

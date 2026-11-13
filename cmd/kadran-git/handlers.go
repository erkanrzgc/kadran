package main

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/github"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// chunkSize, akıtılan arşiv parçalarının en büyük boyutudur.
const chunkSize = 64 << 10

// gitService, handler'ların çağırdığı Service yüzeyidir; testler sahte enjekte eder.
type gitService interface {
	StartDeviceFlow(ctx context.Context) (*kadranv1.GitHubUserCodePrompt, error)
	Status(ctx context.Context) (*kadranv1.GitHubConnection, error)
	Disconnect(ctx context.Context) error
	ResolveBranch(ctx context.Context, owner, repo, branch string) (string, error)
	Decide(ctx context.Context, owner, repo string) (github.Decision, error)
	Tarball(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error)
}

var _ gitService = (*github.Service)(nil)

// statusFor hatayı SABİT metinli bir gRPC durumuna çevirir. Bilinmeyen
// hataların metni asla taşınmaz: imzalı adres ya da jeton içerebilir.
func statusFor(err error) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request timed out")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, github.ErrNotFound):
		return status.Error(codes.NotFound, "not found on GitHub")
	case errors.Is(err, github.ErrForbidden):
		return status.Error(codes.PermissionDenied, "GitHub access denied")
	case errors.Is(err, github.ErrMoved):
		return status.Error(codes.FailedPrecondition, "repository moved: update the app definition")
	case errors.Is(err, github.ErrReconnect), errors.Is(err, github.ErrNotConnected):
		return status.Error(codes.FailedPrecondition, "GitHub is not connected: reconnect GitHub")
	case errors.Is(err, github.ErrRateLimited):
		return status.Error(codes.ResourceExhausted, "GitHub rate limit reached")
	}
	return status.Error(codes.Unavailable, "GitHub request failed")
}

// adminServer yalnız kadrand'ın çağırabildiği yönetim yüzüdür.
type adminServer struct {
	kadranv1.UnimplementedGitAdminServiceServer
	svc gitService
}

func (a *adminServer) StartDeviceFlow(ctx context.Context, _ *kadranv1.StartDeviceFlowRequest) (*kadranv1.StartDeviceFlowResponse, error) {
	p, err := a.svc.StartDeviceFlow(ctx)
	if err != nil {
		return nil, statusFor(err)
	}
	return &kadranv1.StartDeviceFlowResponse{Prompt: p}, nil
}

func (a *adminServer) GetGitStatus(ctx context.Context, _ *kadranv1.GetGitStatusRequest) (*kadranv1.GetGitStatusResponse, error) {
	c, err := a.svc.Status(ctx)
	if err != nil {
		return nil, statusFor(err)
	}
	return &kadranv1.GetGitStatusResponse{Connection: c}, nil
}

func (a *adminServer) DisconnectGit(ctx context.Context, _ *kadranv1.DisconnectGitRequest) (*kadranv1.DisconnectGitResponse, error) {
	if err := a.svc.Disconnect(ctx); err != nil {
		return nil, statusFor(err)
	}
	return &kadranv1.DisconnectGitResponse{}, nil
}

// ResolveBranch bir ERİŞİM DENETİMİ DEĞİLDİR. Derlemeler Decide → Tarball
// yolundan geçer; ResolveBranch yalnız yöneticiye açık bir kadrand çağrısı
// için dalı commit karmasına çevirir.
func (a *adminServer) ResolveBranch(ctx context.Context, req *kadranv1.ResolveBranchRequest) (*kadranv1.ResolveBranchResponse, error) {
	if !github.ValidRepoRef(req.GetOwner(), req.GetRepo()) {
		return nil, status.Error(codes.InvalidArgument, "invalid repository reference")
	}
	if !github.ValidBranch(req.GetBranch()) {
		return nil, status.Error(codes.InvalidArgument, "invalid branch name")
	}
	sha, err := a.svc.ResolveBranch(ctx, req.GetOwner(), req.GetRepo(), req.GetBranch())
	if err != nil {
		return nil, statusFor(err)
	}
	return &kadranv1.ResolveBranchResponse{CommitSha: sha}, nil
}

// fetchServer yalnız root'un (yürütücü) çağırabildiği arşiv yüzüdür.
type fetchServer struct {
	kadranv1.UnimplementedGitFetchServiceServer
	svc gitService
}

func decisionMsg(d kadranv1.FetchArchiveResponse_Decision) *kadranv1.FetchArchiveResponse {
	return &kadranv1.FetchArchiveResponse{Kind: &kadranv1.FetchArchiveResponse_Decision_{Decision: d}}
}

// FetchArchive durum makinesi (git.proto): karardan ÖNCEKİ her hata ileti
// göndermeden döner (yürütücü herkese açık yola düşer); karardan SONRAKİ her
// hata derlemeyi düşürür. NOT_* kararları akıştaki tek iletidir.
func (f *fetchServer) FetchArchive(req *kadranv1.FetchArchiveRequest, stream kadranv1.GitFetchService_FetchArchiveServer) error {
	owner, repo, sha := req.GetOwner(), req.GetRepo(), req.GetCommitSha()
	if !github.ValidRepoRef(owner, repo) || !github.ValidSHA(sha) {
		return status.Error(codes.InvalidArgument, "invalid repository reference")
	}
	ctx := stream.Context()
	d, err := f.svc.Decide(ctx, owner, repo)
	if err != nil {
		return preDecisionStatus(err)
	}
	var msg kadranv1.FetchArchiveResponse_Decision
	switch d {
	case github.DecisionNotConnected:
		return f.sendOnly(stream, kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED)
	case github.DecisionNotCovered:
		return f.sendOnly(stream, kadranv1.FetchArchiveResponse_DECISION_NOT_COVERED)
	case github.DecisionArchive:
		msg = kadranv1.FetchArchiveResponse_DECISION_ARCHIVE
	default:
		return status.Error(codes.Unavailable, "unknown decision")
	}
	// Tarball'ı karar iletisinden ÖNCE açmak karardan önceki hata sayılırdı;
	// sıra: karar gönderilir, sonra arşiv açılır.
	if err := stream.Send(decisionMsg(msg)); err != nil {
		return statusFor(sendErr(ctx))
	}
	body, err := f.svc.Tarball(ctx, owner, repo, sha)
	if err != nil {
		return statusFor(err)
	}
	defer func() { _ = body.Close() }()
	return streamBody(stream, body)
}

func (f *fetchServer) sendOnly(stream kadranv1.GitFetchService_FetchArchiveServer, d kadranv1.FetchArchiveResponse_Decision) error {
	if err := stream.Send(decisionMsg(d)); err != nil {
		return statusFor(sendErr(stream.Context()))
	}
	return nil
}

// preDecisionStatus karardan önceki hatayı çevirir; yalnız Unavailable ya da
// bağlam kodları döner (yürütücü hepsinde herkese açık yola düşer).
func preDecisionStatus(err error) error {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return statusFor(err)
	}
	return status.Error(codes.Unavailable, "GitHub coverage check failed")
}

// sendErr, Send hatasının metnini taşımaz; bağlam bittiyse onu, yoksa genel
// bir taşıma hatasını döner.
func sendErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("stream send failed")
}

// streamBody gövdeyi en çok chunkSize'lık parçalarla akıtır. EOF akışı bitirir.
func streamBody(stream kadranv1.GitFetchService_FetchArchiveServer, body io.Reader) error {
	buf := make([]byte, chunkSize)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			// Send iletiyi dönmeden önce serileştirir; buf yeniden kullanılabilir.
			m := &kadranv1.FetchArchiveResponse{Kind: &kadranv1.FetchArchiveResponse_Chunk{Chunk: buf[:n]}}
			if serr := stream.Send(m); serr != nil {
				return statusFor(sendErr(stream.Context()))
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return statusFor(err)
		}
	}
}

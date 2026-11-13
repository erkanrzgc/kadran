package exec

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	// defaultGitFetchSocket kod sabitidir, bayrak DEĞİL: birimin ExecStart'ına
	// yazılırsa --allow-repo drop-in'i onu düşürürdü.
	defaultGitFetchSocket = "/run/kadran-git/fetch.sock"
	// gitDecisionTimeout, bağlanma + ilk ileti için toplam süredir.
	gitDecisionTimeout = 5 * time.Second
)

// GitFetcher, ayrıcalıksız kadran-git'ten özel depo arşivi ister.
//
// Karardan ÖNCEKİ her başarısızlık (bağlanamama, süre aşımı, gRPC hatası,
// protokol ihlali) NOT_CONNECTED sayılır: herkese açık yol kimlik bilgisi
// taşımaz, bu yüzden güvenli. ARCHIVE'dan SONRAKİ her başarısızlık err'dir.
type GitFetcher interface {
	Fetch(ctx context.Context, src *kadranv1.GitSource) (kadranv1.FetchArchiveResponse_Decision, *os.File, error)
}

type gitFetcher struct {
	socket          string
	decisionTimeout time.Duration
}

// NewGitFetcher, soket yoluna bağlanan bir GitFetcher döner.
func NewGitFetcher(socketPath string) GitFetcher {
	return &gitFetcher{socket: socketPath, decisionTimeout: gitDecisionTimeout}
}

// preDecision, karardan önceki başarısızlığı günlüğe yazıp NOT_CONNECTED döner.
// Sunucunun durum metni ASLA günlüğe girmez; yalnız sabit kategori.
func preDecision(reason string) (kadranv1.FetchArchiveResponse_Decision, *os.File, error) {
	slog.Warn("git fetch service unavailable, using public build path", "reason", reason)
	return kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED, nil, nil
}

func (g *gitFetcher) Fetch(ctx context.Context, src *kadranv1.GitSource) (kadranv1.FetchArchiveResponse_Decision, *os.File, error) {
	conn, err := grpc.NewClient("unix:"+g.socket, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return preDecision("dial")
	}
	defer conn.Close()
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var fired atomic.Bool
	timer := time.AfterFunc(g.decisionTimeout, func() { fired.Store(true); cancel() })
	defer timer.Stop()

	stream, err := kadranv1.NewGitFetchServiceClient(conn).FetchArchive(sctx, &kadranv1.FetchArchiveRequest{
		Owner: src.GetOwner(), Repo: src.GetRepo(), CommitSha: src.GetCommitSha(),
	})
	if err != nil {
		return preDecision("dial")
	}
	first, err := stream.Recv()
	if !timer.Stop() || fired.Load() {
		return preDecision("timeout")
	}
	if err != nil {
		return preDecision("status")
	}
	dec, ok := first.GetKind().(*kadranv1.FetchArchiveResponse_Decision_)
	if !ok {
		return preDecision("protocol")
	}
	switch dec.Decision {
	case kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED, kadranv1.FetchArchiveResponse_DECISION_NOT_COVERED:
		return dec.Decision, nil, nil // defer cancel akışı keser; devamı okunmaz
	case kadranv1.FetchArchiveResponse_DECISION_ARCHIVE:
		f, err := receiveArchive(ctx, stream)
		return kadranv1.FetchArchiveResponse_DECISION_ARCHIVE, f, err
	}
	return preDecision("protocol")
}

// receiveArchive, ARCHIVE'dan sonraki parçaları yeniden yazıcıya akıtır.
// Her başarısızlıkta geçici dosya kapatılıp silinir.
func receiveArchive(ctx context.Context, stream grpc.ServerStreamingClient[kadranv1.FetchArchiveResponse]) (*os.File, error) {
	f, err := os.CreateTemp("", "kadran-context-*.tar")
	if err != nil {
		return nil, errors.New("could not create private build context")
	}
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		rerr := rewritePrivateArchive(ctx, pr, f, defaultArchiveLimits)
		_ = pr.CloseWithError(errors.New("rewrite finished")) // yazıcıyı serbest bırak
		done <- rerr
	}()
	streamErr := pumpChunks(stream, pw)
	rewriteErr := <-done // dosya kapanmadan önce yeniden yazıcının bitmesi şart
	if streamErr == nil {
		streamErr = rewriteErr
	}
	if streamErr == nil {
		if _, serr := f.Seek(0, io.SeekStart); serr != nil {
			streamErr = errors.New("could not rewind private build context")
		}
	}
	if streamErr != nil {
		closePrivateContext(f)
		return nil, streamErr
	}
	return f, nil
}

// pumpChunks yalnız chunk kabul eder. EOF yazıcıyı kapatır; başka her şey
// yazıcıya hata verir (yarım arşiv yeniden yazımda da yakalanır).
func pumpChunks(stream grpc.ServerStreamingClient[kadranv1.FetchArchiveResponse], pw *io.PipeWriter) error {
	bad := errors.New("private archive download failed")
	for {
		m, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			_ = pw.Close()
			return nil
		}
		if err != nil {
			_ = pw.CloseWithError(bad)
			return statusText(err)
		}
		c, ok := m.GetKind().(*kadranv1.FetchArchiveResponse_Chunk)
		if !ok {
			_ = pw.CloseWithError(bad)
			return bad
		}
		if _, werr := pw.Write(c.Chunk); werr != nil {
			return nil // yeniden yazıcı erken bitti; hatasını çağıran okur
		}
	}
}

// statusText, sunucu metnini taşımadan koda göre sabit İngilizce metin verir.
func statusText(err error) error {
	switch status.Code(err) {
	case codes.NotFound:
		return errors.New("private repository not found or app not installed on it")
	case codes.PermissionDenied:
		return errors.New("app lacks permission for this repository")
	case codes.FailedPrecondition:
		return errors.New("GitHub connection needs attention: run kadran github status")
	case codes.ResourceExhausted:
		return errors.New("GitHub rate limit reached, retry later")
	}
	return errors.New("private archive download failed")
}

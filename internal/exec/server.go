package exec

import (
	"context"
	"errors"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/pbconv"
	"github.com/erkanrzgc/panely/internal/version"
)

// Server, ExecutorService'i uygular.
//
// UnimplementedExecutorServiceServer KASITLI OLARAK GÖMÜLMEZ.
// buf.gen.yaml'da require_unimplemented_servers=false ayarlıdır; bu
// sayede exec.proto'ya yeni bir ayrıcalıklı RPC eklendiğinde bu dosya
// DERLENMEZ ve uygulanması unutulamaz. Ayrıntı: docs/decisions.md K-002.
type Server struct {
	journal      *Journal
	dockerSocket string
}

// ServerOptions, executor sunucusunu yapılandırır.
type ServerOptions struct {
	// Journal, executor'ın kendi hash-zincirli denetim günlüğü. Zorunlu.
	Journal *Journal

	// DockerSocket, Docker Engine API soketi. Boşsa varsayılan kullanılır.
	DockerSocket string
}

// NewServer, executor servisini oluşturur.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Journal == nil {
		return nil, errors.New("exec: denetim günlüğü zorunludur")
	}
	if opts.DockerSocket == "" {
		opts.DockerSocket = DefaultDockerSocket
	}
	return &Server{journal: opts.Journal, dockerSocket: opts.DockerSocket}, nil
}

// ── Denetim politikası ───────────────────────────────────────────────
//
// Yalnızca DURUM DEĞİŞTİREN işlemler günlüğe yazılır. Ping, GetHostInfo ve
// ReadAuditJournal salt okunurdur ve kaydedilmez.
//
// Gerekçe: panelyd bu uçları düzenli olarak çağırır (durum ekranı, zincir
// doğrulaması). Her çağrıyı kaydetmek günlüğü gürültüyle doldurur ve asıl
// önemli olan ayrıcalıklı işlemleri görünmez kılar. Denetim günlüğünün
// değeri okunabilirliğinde; her şeyi kaydeden bir günlük hiçbir şey
// kaydetmemeye yaklaşır.
//
// Faz 1'de gelen ContainerCreate, ImageBuild gibi işlemlerin HEPSİ
// kaydedilir — reddedilenler dahil (AUDIT_OUTCOME_DENIED).

// Ping, executor'ın canlılığını, sürümünü ve efektif kullanıcısını döner.
func (s *Server) Ping(context.Context, *panelyv1.ExecutorServicePingRequest) (*panelyv1.ExecutorServicePingResponse, error) {
	return &panelyv1.ExecutorServicePingResponse{
		ExecutorVersion: version.Version,
		ProtocolVersion: version.Protocol,
		// Kurulum doğrulaması için: panelyd bu değeri kontrol ederek
		// executor'ın gerçekten ayrıcalıklı çalıştığını teyit eder.
		EffectiveUid: uint32(os.Geteuid()), //nolint:gosec // uid daima 32 bite sığar
	}, nil
}

// GetHostInfo, çekirdek, bellek ve Docker bilgisini döner. Salt okunur.
func (s *Server) GetHostInfo(ctx context.Context, _ *panelyv1.GetHostInfoRequest) (*panelyv1.GetHostInfoResponse, error) {
	return &panelyv1.GetHostInfoResponse{
		Host: collectHostInfo(ctx, s.dockerSocket),
	}, nil
}

// ReadAuditJournal, executor'ın kendi denetim zincirini döner.
//
// panelyd bunu kendi SQLite zinciriyle karşılaştırmak için çağırır.
// Journal.Read her çağrıda zinciri seq 1'den yeniden doğruladığı için
// buradan dönen kayıtlar okuma anında kanıtlanmış olur.
func (s *Server) ReadAuditJournal(_ context.Context, req *panelyv1.ReadAuditJournalRequest) (*panelyv1.ReadAuditJournalResponse, error) {
	records, err := s.journal.Read(req.GetAfterSeq(), int(req.GetLimit()))
	if err != nil {
		// Zincir doğrulanamıyorsa bu bir iç tutarsızlık değil, güvenlik
		// olayıdır. DataLoss kodu çağırana durumun ciddiyetini bildirir.
		return nil, status.Errorf(codes.DataLoss, "denetim günlüğü okunamadı: %v", err)
	}

	latestSeq, _ := s.journal.Head()
	return &panelyv1.ReadAuditJournalResponse{
		Records:   pbconv.AuditRecordsToProto(records),
		LatestSeq: latestSeq,
	}, nil
}

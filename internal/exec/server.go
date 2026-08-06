package exec

import (
	"context"
	"errors"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/dockerdrv"
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

	// docker, Engine API sürücüsü. Proto mesajı KABUL ETMEZ — doğrulama
	// bu paketin işi ve o sınırın kodla değil TİPLE korunması gerekiyor
	// (internal/dockerdrv paket yorumu).
	docker *dockerdrv.Client

	// allowedGitHosts, ImageBuild'in kabul ettiği kaynak sunucularıdır.
	//
	// Executor'ın YAPILANDIRMASINDAN gelir, istekten değil. Ele geçirilmiş
	// bir panelyd listeye ekleme yapamamalıdır; `-docker-socket` ve
	// `-owner-group` ile aynı desen.
	allowedGitHosts []string
}

// ServerOptions, executor sunucusunu yapılandırır.
type ServerOptions struct {
	// Journal, executor'ın kendi hash-zincirli denetim günlüğü. Zorunlu.
	Journal *Journal

	// DockerSocket, Docker Engine API soketi. Boşsa varsayılan kullanılır.
	DockerSocket string

	// AllowedGitHosts, ImageBuild'in kabul edeceği kaynak sunucuları.
	// Boşsa yalnızca DefaultGitHost kabul edilir.
	AllowedGitHosts []string

	// VolumeRoot, uygulama hacimlerinin kökü. Boşsa varsayılan kullanılır.
	//
	// Bu dizinin `nodev,nosuid` ile bağlanmış olması gerekir; sürücü bunu
	// hacim bağlamadan önce çalışma anında DOĞRULAR (K-039).
	VolumeRoot string
}

// DefaultVolumeRoot, uygulama hacimlerinin varsayılan köküdür.
//
// tmpfiles bu dizini oluşturur, var-lib-panely-volumes.mount birimi
// `bind,nodev,nosuid` ile bağlar.
const DefaultVolumeRoot = "/var/lib/panely/volumes"

// NewServer, executor servisini oluşturur.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Journal == nil {
		return nil, errors.New("exec: denetim günlüğü zorunludur")
	}
	if opts.DockerSocket == "" {
		opts.DockerSocket = DefaultDockerSocket
	}
	// Boş liste "her host serbest" ANLAMINA GELMEZ. Varsayılana düşülür:
	// boş bir beyaz listeyi "kısıt yok" saymak, yapılandırma hatasını
	// sessizce güvenlik açığına çevirirdi.
	if len(opts.AllowedGitHosts) == 0 {
		opts.AllowedGitHosts = []string{DefaultGitHost}
	}
	if opts.VolumeRoot == "" {
		opts.VolumeRoot = DefaultVolumeRoot
	}
	return &Server{
		journal:         opts.Journal,
		dockerSocket:    opts.DockerSocket,
		allowedGitHosts: opts.AllowedGitHosts,
		docker:          dockerdrv.New(opts.DockerSocket, opts.VolumeRoot),
	}, nil
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
		Host: collectHostInfo(ctx, s.docker),
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

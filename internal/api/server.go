// Package api, iş istasyonu istemcisine (CLI / Electron sidecar) açılan
// PanelyService'i uygular.
//
// Bu paket YETKİSİZ süreçte çalışır. Docker'a, ayrıcalıklı dosya sistemine
// veya rastgele komutlara erişimi yoktur; ayrıcalık gerektiren her şey
// executor'a tipli bir RPC olarak gider.
package api

import (
	"context"
	"errors"
	"os"
	"os/user"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/pbconv"
	"github.com/erkanrzgc/panely/internal/store"
	"github.com/erkanrzgc/panely/internal/version"
)

// Server, PanelyService'i uygular.
//
// UnimplementedPanelyServiceServer kasıtlı olarak GÖMÜLMEZ; gerekçe için
// docs/decisions.md K-011'e bakın.
type Server struct {
	store     *store.Store
	exec      *execclient.Client
	startedAt time.Time
	runAsUser string
}

// ServerOptions, API sunucusunu yapılandırır.
type ServerOptions struct {
	// Store, kontrol düzlemi veritabanı. Zorunlu.
	Store *store.Store

	// Executor, ayrıcalıklı executor istemcisi. Zorunlu.
	Executor *execclient.Client
}

// NewServer, API servisini oluşturur.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.Store == nil {
		return nil, errors.New("api: veritabanı zorunludur")
	}
	if opts.Executor == nil {
		return nil, errors.New("api: executor istemcisi zorunludur")
	}

	// Hangi kullanıcı olarak çalıştığımız durum ekranında gösterilir:
	// "root" görünüyorsa kurulum bozuk demektir ve bu hemen fark edilmeli.
	runAs := "unknown"
	if u, err := user.Current(); err == nil {
		runAs = u.Username
	}

	return &Server{
		store:     opts.Store,
		exec:      opts.Executor,
		startedAt: time.Now(),
		runAsUser: runAs,
	}, nil
}

// Ping, bağlantı canlılığını ve sürüm uyumunu doğrular.
func (s *Server) Ping(_ context.Context, req *panelyv1.PingRequest) (*panelyv1.PingResponse, error) {
	resp := &panelyv1.PingResponse{
		DaemonVersion:   version.Version,
		ProtocolVersion: version.Protocol,
		ServerTime:      timestamppb.Now(),
	}

	// İstemci sürümü farklıysa engellemeyiz ama uyarırız: protokol sürümü
	// aynı olduğu sürece iletişim geçerlidir. Asıl uyumsuzluk denetimini
	// istemci, ProtocolVersion'a bakarak yapar.
	if cv := req.GetClientVersion(); cv != "" && cv != version.Version {
		resp.CompatibilityWarning = "istemci sürümü " + cv +
			", daemon sürümü " + version.Version + " — ikisini birlikte güncellemek önerilir"
	}
	return resp, nil
}

// GetSystemInfo, daemon ve host durumunu döner.
func (s *Server) GetSystemInfo(ctx context.Context, _ *panelyv1.GetSystemInfoRequest) (*panelyv1.GetSystemInfoResponse, error) {
	resp := &panelyv1.GetSystemInfoResponse{
		DaemonVersion:       version.Version,
		DaemonUptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		RunningAsUser:       s.runAsUser,
	}
	if h, err := os.Hostname(); err == nil {
		resp.Hostname = h
	}

	// Executor'a ulaşılamaması ölümcül değildir: durum ekranı bunu
	// göstermeli ki kullanıcı sorunu görebilsin. Hata döndürüp ekranı
	// tamamen boş bırakmak, teşhisi zorlaştırırdı.
	ctx, cancel := context.WithTimeout(ctx, execclient.DefaultTimeout)
	defer cancel()

	ping, err := s.exec.Ping(ctx)
	if err != nil {
		return resp, nil
	}
	resp.ExecutorReachable = true
	resp.ExecutorVersion = ping.Version

	if host, err := s.exec.HostInfo(ctx); err == nil {
		resp.Host = host
	}
	return resp, nil
}

// ListAuditRecords, denetim zincirini sayfalı olarak döner.
func (s *Server) ListAuditRecords(ctx context.Context, req *panelyv1.ListAuditRecordsRequest) (*panelyv1.ListAuditRecordsResponse, error) {
	records, err := s.store.ListAudit(ctx, req.GetAfterSeq(), int(req.GetLimit()))
	if err != nil {
		return nil, status.Errorf(codes.Internal, "denetim kayıtları okunamadı: %v", err)
	}

	latestSeq, _, err := s.store.AuditHead(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "zincir başı okunamadı: %v", err)
	}

	return &panelyv1.ListAuditRecordsResponse{
		Records:   pbconv.AuditRecordsToProto(records),
		LatestSeq: latestSeq,
	}, nil
}

// VerifyAuditChain, iki bağımsız zinciri de doğrular.
//
// # Neden iki zincir?
//
// Daemon'ın SQLite zinciri ile executor'ın dosya zinciri ayrı tutulur.
// panelyd'nin ele geçirilmesi tehdit modelinin merkezinde: kayıtlar
// yalnızca panelyd'de olsaydı, ele geçirilmiş bir panelyd kendi yaptığı
// ayrıcalıklı çağrıları hiç kaydetmeyebilirdi. Executor kendi günlüğüne
// yazar ve panelyd o dosyaya YAZAMAZ (0640 root:panely).
//
// Faz 1 notu: şu anda iki zincir bağımsız olarak doğrulanıyor ama
// birbirlerine ÇAPRAZ REFERANSLI değil. Faz 1'de executor'ın yanıtı
// yazdığı kaydın hash'ini döndürecek, panelyd bunu kendi kaydında
// saklayacak ve doğrulama "her executor kaydının daemon tarafında bir
// karşılığı var mı" sorusunu kesin olarak yanıtlayabilecek. Faz 0'da
// durum değiştiren executor çağrısı olmadığı için executor zinciri boş.
func (s *Server) VerifyAuditChain(ctx context.Context, _ *panelyv1.VerifyAuditChainRequest) (*panelyv1.VerifyAuditChainResponse, error) {
	resp := &panelyv1.VerifyAuditChainResponse{}

	checked, err := s.store.VerifyAuditChain(ctx)
	resp.RecordsChecked = checked
	if err != nil {
		resp.Valid = false
		// Kopma noktası: doğrulanan son kayıttan sonraki kayıt.
		resp.FirstInvalidSeq = checked + 1
		resp.Detail = err.Error()
	} else {
		resp.Valid = true
		resp.Detail = "daemon zinciri geçerli"
	}

	resp.ExecutorChainValid, resp.ExecutorDetail = s.verifyExecutorChain(ctx)
	return resp, nil
}

// verifyExecutorChain, executor'ın kendi günlüğünü baştan sona doğrular.
func (s *Server) verifyExecutorChain(ctx context.Context) (bool, string) {
	ctx, cancel := context.WithTimeout(ctx, execclient.DefaultTimeout)
	defer cancel()

	const pageSize = 500

	v := audit.NewVerifier()
	var after uint64

	for {
		page, err := s.exec.ReadJournal(ctx, after, pageSize)
		if err != nil {
			return false, "executor günlüğü okunamadı: " + err.Error()
		}
		if len(page.Records) == 0 {
			if v.Count() == 0 {
				return true, "executor zinciri boş (henüz ayrıcalıklı işlem yapılmadı)"
			}
			return true, "executor zinciri geçerli"
		}
		for _, rec := range page.Records {
			if err := v.Next(rec); err != nil {
				return false, err.Error()
			}
		}
		after = page.Records[len(page.Records)-1].Seq
	}
}

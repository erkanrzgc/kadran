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
	exec      Executor
	startedAt time.Time
	runAsUser string
}

// Executor, panelyd'nin ayrıcalıklı executor'dan ihtiyaç duyduğu yüzeydir.
//
// Arayüz KULLANILDIĞI yerde tanımlanıyor, uygulandığı yerde değil:
// *execclient.Client onu kendiliğinden karşılıyor.
//
// Gerekçe testten geliyor ve gerçek: dağıtım akışının davranışları
// (derleme yarıda ölürse sürüm mühürleniyor mu, istemci koparsa ne
// oluyor) yalnızca executor'ın cevabı KONTROL EDİLEBİLİRSE sınanabilir.
// Somut tipe bağlı kalsaydık, bu yolları sınamanın tek yolu gerçek bir
// Docker daemon'ı olurdu — yani birim testinde HİÇ sınanmazdı.
type Executor interface {
	Ping(ctx context.Context) (execclient.PingResult, error)
	HostInfo(ctx context.Context) (*panelyv1.HostInfo, error)
	ReadJournal(ctx context.Context, afterSeq uint64, limit uint32) (execclient.JournalPage, error)
	ImageBuild(ctx context.Context, req *panelyv1.ImageBuildRequest, sink execclient.BuildSink) (string, error)
}

// ServerOptions, API sunucusunu yapılandırır.
type ServerOptions struct {
	// Store, kontrol düzlemi veritabanı. Zorunlu.
	Store *store.Store

	// Executor, ayrıcalıklı executor istemcisi. Zorunlu.
	Executor Executor
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
// Doğrulama sonucu neden bool DEĞİL?
//
// Üç ayrı durum var ve ikisini karıştırmak pahalıya patlar:
//
//   - VALID       — zincir doğrulandı.
//   - INVALID     — zincir kırık. Kurcalama şüphesi, araştırılmalı.
//   - UNREACHABLE — doğrulanamadı (executor kapalı, veritabanı okunamadı).
//     Zincir hakkında HİÇBİR ŞEY söylemez.
//
// "Doğrulanamadı"yı "geçersiz" diye raporlamak, executor'ın kapalı olduğu
// her an operatörü olmayan bir saldırının peşine düşürürdü.
func (s *Server) VerifyAuditChain(ctx context.Context, _ *panelyv1.VerifyAuditChainRequest) (*panelyv1.VerifyAuditChainResponse, error) {
	resp := &panelyv1.VerifyAuditChainResponse{}

	checked, err := s.store.VerifyAuditChain(ctx)
	resp.RecordsChecked = checked

	switch {
	case err == nil:
		resp.DaemonStatus = panelyv1.ChainStatus_CHAIN_STATUS_VALID
		resp.Detail = "daemon zinciri geçerli"

	case errors.Is(err, audit.ErrChainBroken):
		// Zincirin kendisi kırık: bu gerçek bir bulgu.
		resp.DaemonStatus = panelyv1.ChainStatus_CHAIN_STATUS_INVALID
		// Kopma noktası: doğrulanan son kayıttan sonraki kayıt.
		resp.FirstInvalidSeq = checked + 1
		resp.Detail = err.Error()

	default:
		// Veritabanı okunamadı. Zincir hakkında bir iddiada BULUNMUYORUZ.
		resp.DaemonStatus = panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE
		resp.Detail = "daemon zinciri doğrulanamadı: " + err.Error()
	}

	execStatus, execChecked, execDetail := s.verifyExecutorChain(ctx)
	resp.ExecutorStatus = execStatus
	resp.ExecutorRecordsChecked = execChecked
	resp.ExecutorDetail = execDetail
	return resp, nil
}

// verifyExecutorChain, executor'ın kendi günlüğünü baştan sona doğrular.
func (s *Server) verifyExecutorChain(ctx context.Context) (panelyv1.ChainStatus, uint64, string) {
	ctx, cancel := context.WithTimeout(ctx, execclient.DefaultTimeout)
	defer cancel()

	const pageSize = 500

	v := audit.NewVerifier()
	var after uint64

	for {
		page, err := s.exec.ReadJournal(ctx, after, pageSize)
		if err != nil {
			// Executor'a ulaşılamıyor: günlüğü hakkında bir şey bilmiyoruz.
			// Hata zaten "executor denetim günlüğü okunamadı" diye
			// başlıyor; başına bir kez daha eklemek mesajı okunmaz yapar.
			return panelyv1.ChainStatus_CHAIN_STATUS_UNREACHABLE, v.Count(), err.Error()
		}
		if len(page.Records) == 0 {
			if v.Count() == 0 {
				return panelyv1.ChainStatus_CHAIN_STATUS_VALID, 0,
					"executor zinciri boş (henüz ayrıcalıklı işlem yapılmadı)"
			}
			return panelyv1.ChainStatus_CHAIN_STATUS_VALID, v.Count(),
				"executor zinciri geçerli"
		}
		for _, rec := range page.Records {
			if err := v.Next(rec); err != nil {
				return panelyv1.ChainStatus_CHAIN_STATUS_INVALID, v.Count(), err.Error()
			}
		}
		after = page.Records[len(page.Records)-1].Seq
	}
}

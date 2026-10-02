package client

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// ── Test altyapısı ───────────────────────────────────────────────────

// singleConnListener, tek bir hazır bağlantıyı sunan net.Listener'dır.
// gRPC sunucusu Serve(listener) beklediği için gerekli.
type singleConnListener struct {
	conn net.Conn
	once sync.Once
	done chan struct{}
}

func newSingleConnListener(c net.Conn) *singleConnListener {
	return &singleConnListener{conn: c, done: make(chan struct{})}
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	var conn net.Conn
	l.once.Do(func() { conn = l.conn })
	if conn != nil {
		return conn, nil
	}
	<-l.done
	return nil, net.ErrClosed
}

func (l *singleConnListener) Close() error {
	select {
	case <-l.done:
	default:
		close(l.done)
	}
	return nil
}

func (l *singleConnListener) Addr() net.Addr { return pipeAddr{network: "pipe", address: "test"} }

// stubService, KadranServiceServer'ın en küçük uygulaması.
//
// require_unimplemented_servers=false olduğu için dört metodun da
// yazılması gerekiyor — bu kasıtlı bir tasarım (docs/decisions.md K-011).
type stubService struct {
	pingCalls int
}

// StreamLogs, bu pakette KULLANILMIYOR ve sessizce başarı dönmüyor.
//
// Boş bir gövde "akış çalışıyor" izlenimi verirdi; bu paketin sınadığı
// şey taşıma katmanı, günlük akışı değil.
func (s *stubService) StreamLogs(
	*kadranv1.StreamLogsRequest,
	grpc.ServerStreamingServer[kadranv1.StreamLogsResponse],
) error {
	return errors.New("stubService: StreamLogs beklenmiyordu")
}

func (s *stubService) DeleteApp(
	context.Context, *kadranv1.DeleteAppRequest,
) (*kadranv1.DeleteAppResponse, error) {
	return nil, errors.New("stubService: DeleteApp beklenmiyordu")
}

// PruneApp de sessizce başarı DÖNMÜYOR. Bu stub taşıma katmanını
// sınıyor; bir RPC'yi "çalışıyor" gösterip hiçbir şey yapmamak,
// K-011'in önlemek için var olduğu sessiz boşluğun ta kendisi olurdu.
func (s *stubService) PruneApp(
	context.Context, *kadranv1.PruneAppRequest,
) (*kadranv1.PruneAppResponse, error) {
	return nil, errors.New("stubService: PruneApp beklenmiyordu")
}

// Alarm RPC'si de sessizce başarı dönmüyor; gerekçe PruneApp'ta.
func (s *stubService) ListAlarms(
	context.Context, *kadranv1.ListAlarmsRequest,
) (*kadranv1.ListAlarmsResponse, error) {
	return nil, errors.New("stubService: ListAlarms beklenmiyordu")
}

// Yedekleme RPC'leri de sessizce başarı dönmüyor; gerekçe PruneApp'ta.
func (s *stubService) CreateBackup(
	context.Context, *kadranv1.CreateBackupRequest,
) (*kadranv1.CreateBackupResponse, error) {
	return nil, errors.New("stubService: CreateBackup beklenmiyordu")
}

func (s *stubService) ListBackups(
	context.Context, *kadranv1.ListBackupsRequest,
) (*kadranv1.ListBackupsResponse, error) {
	return nil, errors.New("stubService: ListBackups beklenmiyordu")
}

func (s *stubService) Ping(context.Context, *kadranv1.PingRequest) (*kadranv1.PingResponse, error) {
	s.pingCalls++
	return &kadranv1.PingResponse{
		DaemonVersion:   "test",
		ProtocolVersion: 1,
		ServerTime:      timestamppb.Now(),
	}, nil
}

func (s *stubService) GetSystemInfo(context.Context, *kadranv1.GetSystemInfoRequest) (*kadranv1.GetSystemInfoResponse, error) {
	return &kadranv1.GetSystemInfoResponse{DaemonVersion: "test", Hostname: "stub"}, nil
}

func (s *stubService) ListAuditRecords(context.Context, *kadranv1.ListAuditRecordsRequest) (*kadranv1.ListAuditRecordsResponse, error) {
	return &kadranv1.ListAuditRecordsResponse{}, nil
}

// ── Faz 1 uygulama RPC'leri ─────────────────────────────────────────
//
// Bu saplamalar HİÇBİR ŞEY YAPMAZ ve yapmamalı: bu paketin testi taşıma
// katmanını sınıyor (önsöz + gRPC aynı bağlantıda), iş mantığını değil.
//
// Var olma sebepleri buf.gen.yaml'daki `require_unimplemented_servers=false`:
// UnimplementedKadranServiceServer gömülmediği için şemaya eklenen her yeni
// RPC DERLEMEYİ KIRAR. Kırıldı — tam da tasarlandığı gibi. Gömüp geçmek,
// tripwire'ı bu paket için kalıcı olarak devre dışı bırakırdı.

func (s *stubService) CreateApp(context.Context, *kadranv1.CreateAppRequest) (*kadranv1.CreateAppResponse, error) {
	return &kadranv1.CreateAppResponse{}, nil
}

func (s *stubService) UpdateApp(context.Context, *kadranv1.UpdateAppRequest) (*kadranv1.UpdateAppResponse, error) {
	return &kadranv1.UpdateAppResponse{}, nil
}

func (s *stubService) ListApps(context.Context, *kadranv1.ListAppsRequest) (*kadranv1.ListAppsResponse, error) {
	return &kadranv1.ListAppsResponse{}, nil
}

func (s *stubService) GetApp(context.Context, *kadranv1.GetAppRequest) (*kadranv1.GetAppResponse, error) {
	return &kadranv1.GetAppResponse{}, nil
}

func (s *stubService) Deploy(*kadranv1.DeployRequest, grpc.ServerStreamingServer[kadranv1.DeployResponse]) error {
	return status.Error(codes.Unimplemented, "saplama dağıtım yapmaz")
}

func (s *stubService) Rollback(context.Context, *kadranv1.RollbackRequest) (*kadranv1.RollbackResponse, error) {
	return nil, status.Error(codes.Unimplemented, "saplama geri alma yapmaz")
}

func (s *stubService) VerifyAuditChain(context.Context, *kadranv1.VerifyAuditChainRequest) (*kadranv1.VerifyAuditChainResponse, error) {
	return &kadranv1.VerifyAuditChainResponse{
		DaemonStatus:   kadranv1.ChainStatus_CHAIN_STATUS_VALID,
		ExecutorStatus: kadranv1.ChainStatus_CHAIN_STATUS_VALID,
		Detail:         "stub",
	}, nil
}

// connectedPipes, birbirine bağlı iki pipeConn üretir.
//
// io.Pipe kasıtlı olarak kullanılıyor: net.Pipe süre sınırlarını
// DESTEKLER ve gerçek senaryoyu temsil etmezdi. İşletim sistemi boruları
// gibi io.Pipe da süre sınırı tanımaz — test etmek istediğimiz tam olarak
// bu durum.
func connectedPipes() (clientSide, serverSide *pipeConn) {
	clientReader, serverWriter := io.Pipe()
	serverReader, clientWriter := io.Pipe()

	clientSide = newPipeConn(clientReader, clientWriter, "server", nil)
	serverSide = newPipeConn(serverReader, serverWriter, "client", nil)
	return clientSide, serverSide
}

// ── Testler ──────────────────────────────────────────────────────────

// TestGRPCWorksOverPipeConn, ASIL SORUYU yanıtlar: gRPC, süre sınırı
// desteklemeyen bir bağlantı üzerinde çalışır mı?
//
// pipeConn'un SetDeadline metotları os.ErrNoDeadline'a benzer bir hata
// döndürür. gRPC bunları çağırıp hatayı ölümcül sayarsa SSH taşıması hiç
// çalışmazdı. Yorum yazıp geçmek yerine ölçüyoruz.
func TestGRPCWorksOverPipeConn(t *testing.T) {
	clientSide, serverSide := connectedPipes()

	stub := &stubService{}
	server := grpc.NewServer()
	kadranv1.RegisterKadranServiceServer(server, stub)

	listener := newSingleConnListener(serverSide)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serverDone
	})

	conn, err := grpc.NewClient("passthrough:///pipe",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return clientSide, nil
		}),
	)
	if err != nil {
		t.Fatalf("istemci oluşturulamadı: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := kadranv1.NewKadranServiceClient(conn).Ping(ctx, &kadranv1.PingRequest{
		ClientVersion: "test-client",
	})
	if err != nil {
		t.Fatalf("boru üzerinden gRPC çağrısı başarısız: %v", err)
	}
	if resp.GetDaemonVersion() != "test" {
		t.Errorf("daemon sürümü = %q", resp.GetDaemonVersion())
	}
	if stub.pingCalls != 1 {
		t.Errorf("sunucu tarafında ping sayısı = %d, beklenen 1", stub.pingCalls)
	}
}

// TestGRPCSurvivesMultipleCallsOverPipeConn, tek bir boru bağlantısı
// üzerinde ardışık çağrıların çalıştığını doğrular. HTTP/2 çoğullaması
// bağlantıyı yeniden kullanır; ilk çağrının çalışması yeterli kanıt değil.
func TestGRPCSurvivesMultipleCallsOverPipeConn(t *testing.T) {
	clientSide, serverSide := connectedPipes()

	server := grpc.NewServer()
	kadranv1.RegisterKadranServiceServer(server, &stubService{})

	listener := newSingleConnListener(serverSide)
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-serverDone
	})

	conn, err := grpc.NewClient("passthrough:///pipe",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return clientSide, nil
		}),
	)
	if err != nil {
		t.Fatalf("istemci oluşturulamadı: %v", err)
	}
	defer func() { _ = conn.Close() }()

	client := kadranv1.NewKadranServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for i := range 5 {
		if _, err := client.Ping(ctx, &kadranv1.PingRequest{}); err != nil {
			t.Fatalf("%d. çağrı başarısız: %v", i+1, err)
		}
	}

	if _, err := client.GetSystemInfo(ctx, &kadranv1.GetSystemInfoRequest{}); err != nil {
		t.Fatalf("farklı metot çağrısı başarısız: %v", err)
	}
	if _, err := client.VerifyAuditChain(ctx, &kadranv1.VerifyAuditChainRequest{}); err != nil {
		t.Fatalf("doğrulama çağrısı başarısız: %v", err)
	}
}

func TestSetDeadlineReportsUnsupported(t *testing.T) {
	c, _ := connectedPipes()

	// Sessizce başarılı dönmek, çağıranın süre sınırı koyduğunu sanıp
	// koyamamasına yol açardı — bir hata durumunda sonsuz bekleme.
	for name, fn := range map[string]func(time.Time) error{
		"SetDeadline":      c.SetDeadline,
		"SetReadDeadline":  c.SetReadDeadline,
		"SetWriteDeadline": c.SetWriteDeadline,
	} {
		if err := fn(time.Now().Add(time.Second)); err == nil {
			t.Errorf("%s sessizce başarılı döndü", name)
		}
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	c, _ := connectedPipes()

	first := c.Close()
	second := c.Close()

	// Sözleşme: tekrarlanan Close aynı sonucu döndürür. Karşılaştırma
	// errors.Is ile yapılıyor çünkü sonuç errors.Join ile sarmalanmış
	// olabilir ve doğrudan != karşılaştırması sarmalanmış hatada yanılır.
	//
	// Asıl idempotanlık iddiası — temizlik işinin yalnızca bir kez
	// koşması — ayrı bir testte: TestCloseRunsCleanupExactlyOnce.
	if !errors.Is(second, first) {
		t.Errorf("ikinci Close farklı sonuç verdi: %v != %v", second, first)
	}
}

func TestCloseRunsCleanupExactlyOnce(t *testing.T) {
	var calls int
	r, w := io.Pipe()
	c := newPipeConn(r, w, "test", func() error { calls++; return nil })

	_ = c.Close()
	_ = c.Close()
	_ = c.Close()

	if calls != 1 {
		t.Errorf("cleanup çağrı sayısı = %d, beklenen 1", calls)
	}
}

func TestCloseReportsCleanupError(t *testing.T) {
	wantErr := errors.New("alt süreç toplanamadı")
	r, w := io.Pipe()
	c := newPipeConn(r, w, "test", func() error { return wantErr })

	if err := c.Close(); !errors.Is(err, wantErr) {
		t.Errorf("cleanup hatası bildirilmedi: %v", err)
	}
}

func TestAddrsAreDescriptive(t *testing.T) {
	c, _ := connectedPipes()

	if c.RemoteAddr().String() != "server" {
		t.Errorf("uzak adres = %q", c.RemoteAddr().String())
	}
	if c.LocalAddr().Network() != "pipe" {
		t.Errorf("ağ = %q", c.LocalAddr().Network())
	}
}

// TestErrNoDeadlineIsDistinguishable, hatanın standart kütüphanenin
// karşılığıyla karıştırılmadığını doğrular.
func TestErrNoDeadlineIsDistinguishable(t *testing.T) {
	if errors.Is(errNoDeadline, os.ErrNoDeadline) {
		t.Error("errNoDeadline os.ErrNoDeadline ile aynı sayılıyor")
	}
}

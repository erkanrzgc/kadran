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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
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

// stubService, PanelyServiceServer'ın en küçük uygulaması.
//
// require_unimplemented_servers=false olduğu için dört metodun da
// yazılması gerekiyor — bu kasıtlı bir tasarım (docs/decisions.md K-011).
type stubService struct {
	pingCalls int
}

func (s *stubService) Ping(context.Context, *panelyv1.PingRequest) (*panelyv1.PingResponse, error) {
	s.pingCalls++
	return &panelyv1.PingResponse{
		DaemonVersion:   "test",
		ProtocolVersion: 1,
		ServerTime:      timestamppb.Now(),
	}, nil
}

func (s *stubService) GetSystemInfo(context.Context, *panelyv1.GetSystemInfoRequest) (*panelyv1.GetSystemInfoResponse, error) {
	return &panelyv1.GetSystemInfoResponse{DaemonVersion: "test", Hostname: "stub"}, nil
}

func (s *stubService) ListAuditRecords(context.Context, *panelyv1.ListAuditRecordsRequest) (*panelyv1.ListAuditRecordsResponse, error) {
	return &panelyv1.ListAuditRecordsResponse{}, nil
}

func (s *stubService) VerifyAuditChain(context.Context, *panelyv1.VerifyAuditChainRequest) (*panelyv1.VerifyAuditChainResponse, error) {
	return &panelyv1.VerifyAuditChainResponse{
		DaemonStatus:   panelyv1.ChainStatus_CHAIN_STATUS_VALID,
		ExecutorStatus: panelyv1.ChainStatus_CHAIN_STATUS_VALID,
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
	panelyv1.RegisterPanelyServiceServer(server, stub)

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

	resp, err := panelyv1.NewPanelyServiceClient(conn).Ping(ctx, &panelyv1.PingRequest{
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
	panelyv1.RegisterPanelyServiceServer(server, &stubService{})

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

	client := panelyv1.NewPanelyServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	for i := range 5 {
		if _, err := client.Ping(ctx, &panelyv1.PingRequest{}); err != nil {
			t.Fatalf("%d. çağrı başarısız: %v", i+1, err)
		}
	}

	if _, err := client.GetSystemInfo(ctx, &panelyv1.GetSystemInfoRequest{}); err != nil {
		t.Fatalf("farklı metot çağrısı başarısız: %v", err)
	}
	if _, err := client.VerifyAuditChain(ctx, &panelyv1.VerifyAuditChainRequest{}); err != nil {
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

	if !errors.Is(second, first) && second != first {
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

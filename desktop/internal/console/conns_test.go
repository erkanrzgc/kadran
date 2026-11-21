package console

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/version"
)

// sahtePingSunucu, Ping sayısını, TotpRequired değerini ve hata kuyruğunu
// testten yönetilebilir tutar.
type sahtePingSunucu struct {
	kadranv1.UnimplementedKadranServiceServer
	mu       sync.Mutex
	pings    int
	totp     bool
	failNext int
}

func (s *sahtePingSunucu) Ping(context.Context, *kadranv1.PingRequest) (*kadranv1.PingResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pings++
	if s.failNext > 0 {
		s.failNext--
		return nil, errors.New("boom")
	}
	return &kadranv1.PingResponse{ProtocolVersion: version.Protocol, TotpRequired: &s.totp}, nil
}

func (s *sahtePingSunucu) set(totp bool, failNext int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totp, s.failNext = totp, failNext
}

func (s *sahtePingSunucu) pingCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pings
}

// testConns, bufconn'lu sahte sunucuya bağlanan ve dial sayan bir Conns kurar.
func testConns(t *testing.T) (*Conns, *sahtePingSunucu, *atomic.Int32) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	sunucu := &sahtePingSunucu{}
	kadranv1.RegisterKadranServiceServer(srv, sunucu)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	var dials atomic.Int32
	c := NewConns(func(tg client.Target) (*client.Client, error) {
		dials.Add(1)
		return client.DialWithDialer(tg, func(context.Context, string) (net.Conn, error) {
			return lis.Dial()
		})
	})
	t.Cleanup(c.Close)
	return c, sunucu, &dials
}

const testTarget = "kadran-client@h.example"

func TestConnsRepingsEveryCall(t *testing.T) {
	c, sunucu, dials := testConns(t)
	ctx := context.Background()

	_, ping, err := c.Dial(ctx, testTarget)
	if err != nil || ping.GetTotpRequired() {
		t.Fatalf("first dial: ping=%v err=%v", ping, err)
	}
	sunucu.set(true, 0)
	_, ping, err = c.Dial(ctx, testTarget)
	if err != nil || !ping.GetTotpRequired() {
		t.Fatalf("second dial: ping=%v err=%v", ping, err)
	}
	if got := sunucu.pingCount(); got != 2 {
		t.Fatalf("pings = %d, want 2", got)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1", got)
	}
}

func TestConnsRedialsAfterFailure(t *testing.T) {
	c, sunucu, dials := testConns(t)
	ctx := context.Background()
	if _, _, err := c.Dial(ctx, testTarget); err != nil {
		t.Fatal(err)
	}
	sunucu.set(false, 1)
	if _, _, err := c.Dial(ctx, testTarget); err != nil {
		t.Fatalf("redial should succeed: %v", err)
	}
	if got := dials.Load(); got != 2 {
		t.Fatalf("dials = %d, want 2", got)
	}
}

func TestConnsKeepsNothingWhenRedialFails(t *testing.T) {
	c, sunucu, dials := testConns(t)
	ctx := context.Background()
	if _, _, err := c.Dial(ctx, testTarget); err != nil {
		t.Fatal(err)
	}
	sunucu.set(false, 2)
	if _, _, err := c.Dial(ctx, testTarget); err == nil {
		t.Fatal("want error when both pings fail")
	}
	if _, _, err := c.Dial(ctx, testTarget); err != nil {
		t.Fatalf("later dial: %v", err)
	}
	if got := dials.Load(); got != 3 {
		t.Fatalf("dials = %d, want 3 (prime, failed redial, fresh after failure; nothing cached after failure)", got)
	}
}

func TestConnsHandleCloseKeepsConnection(t *testing.T) {
	c, _, dials := testConns(t)
	ctx := context.Background()
	h, _, err := c.Dial(ctx, testTarget)
	if err != nil {
		t.Fatal(err)
	}
	_ = h.Close()
	if _, _, err := c.Dial(ctx, testTarget); err != nil {
		t.Fatal(err)
	}
	if got := dials.Load(); got != 1 {
		t.Fatalf("dials = %d, want 1", got)
	}
}

func TestConnsRejectsBadTarget(t *testing.T) {
	c, _, _ := testConns(t)
	if _, _, err := c.Dial(context.Background(), "-oProxyCommand=x"); err == nil {
		t.Fatal("want parse error")
	}
}

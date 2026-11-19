package client

import (
	"context"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/totp"
	"github.com/erkanrzgc/kadran/internal/version"
)

// sahteSunucu, yalnızca Ping'e cevap verir ve son oturum jetonunu tutar.
type sahteSunucu struct {
	kadranv1.UnimplementedKadranServiceServer
	mu    sync.Mutex
	jeton string
}

func (s *sahteSunucu) Ping(ctx context.Context, _ *kadranv1.PingRequest) (*kadranv1.PingResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jeton = ""
	if v := md.Get(totp.SessionMetadataKey); len(v) > 0 {
		s.jeton = v[0]
	}
	return &kadranv1.PingResponse{ProtocolVersion: version.Protocol}, nil
}

func (s *sahteSunucu) sonJeton() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.jeton
}

// bufconnIstemci, sahte sunucuya bufconn üstünden bağlı bir istemci kurar.
func bufconnIstemci(t *testing.T) (*Client, *sahteSunucu) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	sunucu := &sahteSunucu{}
	kadranv1.RegisterKadranServiceServer(srv, sunucu)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	c, err := DialWithDialer(Target{}, func(context.Context, string) (net.Conn, error) {
		return lis.Dial()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c, sunucu
}

func TestSharedCloseKeepsConn(t *testing.T) {
	owner, _ := bufconnIstemci(t)
	shared := owner.Shared()
	if err := shared.Close(); err != nil {
		t.Fatalf("shared close: %v", err)
	}
	if _, err := owner.CheckProtocol(context.Background()); err != nil {
		t.Fatalf("owner RPC after shared close: %v", err)
	}
}

func TestSharedSessionReachesOwner(t *testing.T) {
	owner, sunucu := bufconnIstemci(t)
	shared := owner.Shared()
	shared.SetSession("t")
	if owner.Session() != "t" {
		t.Fatalf("owner session = %q", owner.Session())
	}
	if _, err := shared.CheckProtocol(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := sunucu.sonJeton(); got != "t" {
		t.Fatalf("server saw token %q", got)
	}
}

func TestSharedOfSharedPointsToOwner(t *testing.T) {
	owner, _ := bufconnIstemci(t)
	second := owner.Shared().Shared()
	second.SetSession("x")
	if owner.Session() != "x" {
		t.Fatalf("owner session = %q", owner.Session())
	}
	if second.Target() != owner.Target() || second.RPC() != owner.RPC() {
		t.Fatal("shared handle must expose the target and RPC of the owner")
	}
}

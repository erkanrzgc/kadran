package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/totp"
)

// ── Oturum önbelleği (K-155) ──────────────────────────────────────────

var oturumAni = time.Unix(1_800_000_000, 0)

func TestSessionRoundTrip(t *testing.T) {
	s := SessionStore{Dir: t.TempDir()}
	if err := s.Save("kadran-client@a.example", "v1.jeton.imza", oturumAni.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	tok, ok, err := s.Load("kadran-client@a.example", oturumAni)
	if err != nil || !ok || tok != "v1.jeton.imza" {
		t.Fatalf("Load = %q %v %v", tok, ok, err)
	}
}

func TestMissingSessionIsNotAnError(t *testing.T) {
	s := SessionStore{Dir: filepath.Join(t.TempDir(), "yok")}
	tok, ok, err := s.Load("kadran-client@a.example", oturumAni)
	if err != nil || ok || tok != "" {
		t.Fatalf("Load = %q %v %v", tok, ok, err)
	}
}

func TestExpiredSessionIsNotLoaded(t *testing.T) {
	s := SessionStore{Dir: t.TempDir()}
	bitis := oturumAni.Add(time.Hour)
	if err := s.Save("h", "v1.a.b", bitis); err != nil {
		t.Fatal(err)
	}
	for _, an := range []time.Time{bitis, bitis.Add(time.Second)} {
		if tok, ok, err := s.Load("h", an); err != nil || ok || tok != "" {
			t.Fatalf("%v anında Load = %q %v %v", an, tok, ok, err)
		}
	}
}

func TestSessionFileMode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sessions")
	s := SessionStore{Dir: dir}
	if err := s.Save("h", "v1.a.b", oturumAni.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("dizinde tek dosya beklenirdi: %v (%v)", entries, err)
	}
	if runtime.GOOS == "windows" {
		return // izin bitleri Windows'ta anlamsız; dosya kullanıcının profilinde
	}
	fi, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("dosya izni %v, beklenen 0600", fi.Mode().Perm())
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Fatalf("dizin izni %v, beklenen 0700", di.Mode().Perm())
	}
}

func TestSaveOverwritesAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	s := SessionStore{Dir: dir}
	for _, tok := range []string{"v1.eski.a", "v1.yeni.b"} {
		if err := s.Save("h", tok, oturumAni.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if tok, ok, err := s.Load("h", oturumAni); err != nil || !ok || tok != "v1.yeni.b" {
		t.Fatalf("Load = %q %v %v", tok, ok, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatalf("geçici dosya kaldı: %v", entries)
	}
}

func TestDeleteMissingIsNil(t *testing.T) {
	s := SessionStore{Dir: t.TempDir()}
	if err := s.Delete("hic-kaydedilmedi"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	if err := s.Save("h", "v1.a.b", oturumAni.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("h"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load("h", oturumAni); ok {
		t.Fatal("silinen oturum yüklendi")
	}
}

func TestTargetsDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	s := SessionStore{Dir: dir}
	if err := s.Save("kadran-client@a.example", "v1.a.a", oturumAni.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.Save("kadran-client@b.example", "v1.b.b", oturumAni.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for hedef, want := range map[string]string{"kadran-client@a.example": "v1.a.a", "kadran-client@b.example": "v1.b.b"} {
		if tok, ok, err := s.Load(hedef, oturumAni); err != nil || !ok || tok != want {
			t.Fatalf("%s: Load = %q %v %v", hedef, tok, ok, err)
		}
	}

	// Dosya adı kısaltılmış bir özet; içindeki hedef başka bir sunucuyu
	// söylüyorsa jeton O sunucunun değildir ve gönderilmez.
	b, err := os.ReadFile(s.path("kadran-client@b.example"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.path("kadran-client@a.example"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if tok, ok, err := s.Load("kadran-client@a.example", oturumAni); err != nil || ok || tok != "" {
		t.Fatalf("başka hedefin jetonu yüklendi: %q %v %v", tok, ok, err)
	}
}

func TestCorruptSessionIsAnError(t *testing.T) {
	s := SessionStore{Dir: t.TempDir()}
	if err := os.WriteFile(s.path("h"), []byte("{bozuk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.Load("h", oturumAni); err == nil || ok {
		t.Fatalf("bozuk dosya: ok=%v err=%v", ok, err)
	}
}

func TestDefaultSessionStoreUnderUserConfig(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("kullanıcı yapılandırma dizini yok: %v", err)
	}
	s, err := DefaultSessionStore()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "kadran", "sessions"); s.Dir != want {
		t.Fatalf("Dir = %q, beklenen %q", s.Dir, want)
	}
}

func TestIsSessionError(t *testing.T) {
	cases := map[error]bool{
		status.Error(codes.Unauthenticated, `TOTP is on for this server: sign in with "kadran login"`): true,
		status.Error(codes.Unauthenticated, `session expired: sign in again with "kadran login"`):      true,
		status.Error(codes.Unauthenticated, `invalid session token: sign in again`):                    true,
		status.Error(codes.PermissionDenied, `session expired: sign in again`):                         false,
		status.Error(codes.Unauthenticated, `no identity`):                                             false,
		errors.New("session expired"): false,
		nil:                           false,
		// Komutlar hatayı sarıyor ("could not get apps: %w"); önek sarılmış
		// mesaja değil sunucunun mesajına bakmalı.
		fmt.Errorf("could not get apps: %w",
			status.Error(codes.Unauthenticated, `session expired: sign in again`)): true,
	}
	for err, want := range cases {
		if got := IsSessionError(err); got != want {
			t.Errorf("IsSessionError(%v) = %v, beklenen %v", err, got, want)
		}
	}
}

// metaKaydedici, sunucuya gelen oturum metadata'sını yöntem başına kaydeder.
type metaKaydedici struct {
	mu    sync.Mutex
	gelen map[string][]string
}

func (m *metaKaydedici) kaydet(ctx context.Context, method string) {
	md, _ := metadata.FromIncomingContext(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gelen[method] = md.Get(totp.SessionMetadataKey)
}

func (m *metaKaydedici) al(method string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gelen[method]
}

// TestInterceptorAttachesToken: SetSession'dan sonra hem tekli hem akış
// çağrıları jetonu taşır; jeton yokken metadata hiç eklenmez.
func TestInterceptorAttachesToken(t *testing.T) {
	clientSide, serverSide := connectedPipes()
	kayit := &metaKaydedici{gelen: map[string][]string{}}
	server := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
			kayit.kaydet(ctx, info.FullMethod)
			return h(ctx, req)
		}),
		grpc.StreamInterceptor(func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, h grpc.StreamHandler) error {
			kayit.kaydet(ss.Context(), info.FullMethod)
			return h(srv, ss)
		}),
	)
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

	c, err := dialWith(Target{SocketPath: "/pipe"}, func(context.Context, string) (net.Conn, error) {
		return clientSide, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := c.RPC().Ping(ctx, &kadranv1.PingRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := kayit.al(kadranv1.KadranService_Ping_FullMethodName); len(got) != 0 {
		t.Fatalf("jeton yokken metadata gitti: %v", got)
	}

	c.SetSession("v1.jeton.imza")
	if _, err := c.RPC().Ping(ctx, &kadranv1.PingRequest{}); err != nil {
		t.Fatal(err)
	}
	if got := kayit.al(kadranv1.KadranService_Ping_FullMethodName); len(got) != 1 || got[0] != "v1.jeton.imza" {
		t.Fatalf("tekli çağrıda jeton: %v", got)
	}
	stream, err := c.RPC().StreamLogs(ctx, &kadranv1.StreamLogsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = stream.Recv() // saplama hata döner; başlıklar o ana kadar gitmiş olur
	if got := kayit.al(kadranv1.KadranService_StreamLogs_FullMethodName); len(got) != 1 || got[0] != "v1.jeton.imza" {
		t.Fatalf("akış çağrısında jeton: %v", got)
	}
}

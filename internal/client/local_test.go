package client

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/erkanrzgc/panely/internal/connproto"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// ── Sahte ssh alt süreci ─────────────────────────────────────────────
//
// Test binary'si kendini `ssh` yerine çalıştırır. Böylece istemcinin
// boruya yazdığı İLK BAYTLAR gerçekten ölçülebiliyor.

const (
	fakeSSHEnv     = "PANELY_TEST_FAKE_SSH"
	fakeSSHOutEnv  = "PANELY_TEST_FAKE_SSH_OUT"
	fakeSSHArgvEnv = "PANELY_TEST_FAKE_SSH_ARGV"
)

// http2Preface, gRPC'nin bağlantıda gönderdiği ilk baytlardır (RFC 7540 §3.5).
const http2Preface = "PRI * HTTP/2.0\r\n\r\nSM\r\n\r\n"

func TestMain(m *testing.M) {
	if os.Getenv(fakeSSHEnv) != "" {
		fakeSSHMain()
		return
	}
	os.Exit(m.Run())
}

// fakeSSHMain, stdin'den gelen ilk baytları dosyaya yazıp çıkar.
// Gerçek ssh'ı taklit etmiyor; yalnızca boruyu dinliyor.
//
// fakeSSHArgvEnv ayarlıysa ayrıca ALDIĞI argümanları kaydeder. Bu,
// "istemci ssh'a ne geçiyor?" sorusunun tek dürüst yanıtıdır: dize
// birleştirmesini okuyup çıkarım yapmak yerine çalıştırılan argv'ye
// bakar.
func fakeSSHMain() {
	if path := os.Getenv(fakeSSHArgvEnv); path != "" {
		_ = os.WriteFile(path, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
	}
	buf := make([]byte, len(http2Preface))
	n, _ := io.ReadFull(os.Stdin, buf)
	_ = os.WriteFile(os.Getenv(fakeSSHOutEnv), buf[:n], 0o600)
}

// ── Yardımcılar ──────────────────────────────────────────────────────

// preambleCreds, api.callerCreds'in peercred'siz eşdeğeridir.
//
// Gerçek sunucu kimliği ayrıca SO_PEERCRED ile doğrular; o yalnızca
// Linux'ta çalışır ve ayrı bir soruyu yanıtlar. Burada sınanan şey şu:
// önsöz okunduktan SONRA gRPC aynı bağlantı üzerinde konuşabiliyor mu?
type preambleCreds struct {
	seen chan connproto.Identity
}

func (c *preambleCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	id, err := connproto.Read(raw)
	if err != nil {
		_ = raw.Close()
		return nil, nil, err
	}
	select {
	case c.seen <- id:
	default:
	}
	return raw, testAuthInfo{}, nil
}

func (c *preambleCreds) ClientHandshake(context.Context, string, net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("istemci tarafında kullanılamaz")
}

func (c *preambleCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "test-preamble"}
}
func (c *preambleCreds) Clone() credentials.TransportCredentials { return c }
func (c *preambleCreds) OverrideServerName(string) error         { return nil }

type testAuthInfo struct{}

func (testAuthInfo) AuthType() string { return "test" }

// ── Testler ──────────────────────────────────────────────────────────

// TestPreambleThenGRPCOverSameConn, ASIL SORUYU yanıtlar: kimlik önsözü
// okunduktan sonra aynı bağlantı üzerinde gRPC çalışıyor mu?
//
// connproto.Read TAM OLARAK önsöz kadar bayt tüketmeli. Bir bayt fazla
// okusa HTTP/2 akışı bozulurdu ve bu ancak uçtan uca bir testte görünür.
//
// Bu test bir HATA yüzünden yazıldı: yerel yol hiç önsöz yazmıyordu.
// panelyd önsözü koşulsuz okuduğu için, sunucuda argümansız `panely
// status` yazmak — yani birincil kullanım — gRPC'nin HTTP/2 önsözünü
// uzunluk sanıp ("PRI " → 1.35 milyar) "önsöz çok büyük" hatasıyla ölürdü.
//
// Boru kullanılıyor çünkü Go, Windows'ta unix soketini dinleyebiliyor ama
// BAĞLANAMIYOR. Gerçek soket üzerindeki karşılığı local_linux_test.go'da.
func TestPreambleThenGRPCOverSameConn(t *testing.T) {
	clientSide, serverSide := connectedPipes()

	creds := &preambleCreds{seen: make(chan connproto.Identity, 1)}
	server := grpc.NewServer(grpc.Creds(creds))
	panelyv1.RegisterPanelyServiceServer(server, &stubService{})

	listener := newSingleConnListener(serverSide)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
		<-done
	})

	conn, err := grpc.NewClient("passthrough:///pipe",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			// Üretimdeki sıranın aynısı: önce önsöz, sonra gRPC.
			// localIdentity() kasten çağrılıyor — sınanan şey üretimde
			// gerçekten gönderilen içerik.
			if err := connproto.Write(clientSide, localIdentity()); err != nil {
				return nil, err
			}
			return clientSide, nil
		}),
	)
	if err != nil {
		t.Fatalf("istemci kurulamadı: %v", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := panelyv1.NewPanelyServiceClient(conn).Ping(ctx, &panelyv1.PingRequest{})
	if err != nil {
		t.Fatalf("önsözden sonra gRPC çağrısı başarısız: %v", err)
	}
	if resp.GetDaemonVersion() != "test" {
		t.Errorf("daemon sürümü = %q", resp.GetDaemonVersion())
	}

	select {
	case id := <-creds.seen:
		if id.Origin != "local" {
			t.Errorf("sunucunun gördüğü origin = %q, beklenen \"local\"", id.Origin)
		}
	default:
		t.Error("sunucu hiç önsöz görmedi")
	}
}

// TestLocalIdentityMakesNoSSHClaims, yerel kimliğin SSH alanlarını boş
// bıraktığını doğrular.
//
// Yerel çağıranın uyduracağı bir SSH parmak izi olmamalı: boş alan
// "bilinmiyor" demektir ve dürüst bir kayıttır. Yer tutucu bir değer,
// denetim izine sonradan bakan birini gerçek bir kimlik gördüğüne
// inandırırdı.
func TestLocalIdentityMakesNoSSHClaims(t *testing.T) {
	id := localIdentity()

	if id.Origin != "local" {
		t.Errorf("origin = %q, beklenen \"local\"", id.Origin)
	}
	if id.Fingerprint != "" {
		t.Errorf("parmak izi uyduruldu: %q", id.Fingerprint)
	}
	if id.SourceIP != "" {
		t.Errorf("kaynak IP uyduruldu: %q", id.SourceIP)
	}
	if id.Label != "" {
		t.Errorf("etiket uyduruldu: %q", id.Label)
	}
}

// TestSSHTransportWritesNoPreamble, SSH yolunun boruya önsöz YAZMADIĞINI
// doğrular.
//
// SSH'ta önsözü sunucuda panely-connect yazıyor. İstemci de yazsaydı
// panelyd iki önsöz görürdü: ilkini okur, ardından HTTP/2 beklediği yerde
// dört baytlık bir uzunluk artı JSON bulurdu.
//
// Bu testin varlık nedeni, ileride birinin "yerelde yazıyoruz, SSH'ta da
// yazalım" diye simetri kurmaya çalışmasını engellemektir.
func TestSSHTransportWritesNoPreamble(t *testing.T) {
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}
	captured := filepath.Join(t.TempDir(), "ilk-baytlar.bin")

	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHOutEnv, captured)

	original := sshCommand
	sshCommand = os.Args[0] // test binary'si kendini ssh yerine çalıştırır
	t.Cleanup(func() { sshCommand = original })

	c, err := Dial(Target{SSHUser: "kullanici", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("istemci kurulamadı: %v", err)
	}
	defer func() { _ = c.Close() }()

	// Çağrının başarısız olması BEKLENİYOR: sahte ssh yanıt vermiyor.
	// Ölçtüğümüz şey sonuç değil, boruya yazılan ilk baytlar.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = c.RPC().Ping(ctx, &panelyv1.PingRequest{})

	data, err := waitForFile(captured, len(http2Preface), 10*time.Second)
	if err != nil {
		t.Fatalf("sahte ssh hiçbir şey yakalamadı: %v", err)
	}

	if got := string(data); got != http2Preface {
		t.Errorf("SSH borusuna yazılan ilk baytlar HTTP/2 önsözü değil.\n"+
			"alınan  : %q\nbeklenen: %q\n"+
			"İstemci kimlik önsözü yazıyorsa bu bir gerileme: SSH yolunda "+
			"önsözü panely-connect yazar.", got, http2Preface)
	}
}

// waitForFile, dosya istenen boyuta ulaşana kadar bekler.
func waitForFile(path string, size int, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && len(data) >= size {
			return data, nil
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = errors.New("dosya beklenen boyuta ulaşmadı")
	}
	return nil, lastErr
}

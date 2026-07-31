//go:build linux

// Bu dosyadaki testler GERÇEK unix soketi gerektirir.
//
// Windows'a alınmadılar çünkü Go orada unix soketini dinleyebiliyor ama
// BAĞLANAMIYOR (autobind desteklenmiyor) — yani `dialLocal` Windows'ta
// hiçbir zaman çalışmaz. Kimlik önsözü ile gRPC'nin bir arada çalıştığı,
// her platformda koşan boru tabanlı testte doğrulanıyor
// (TestPreambleThenGRPCOverSameConn); burada doğrulanan şey ayrı bir soru:
// dialLocal'ın kendisi gerçek taşımada doğru davranıyor mu.
//
// Çalıştırmak için: scripts/test-linux.sh

package client

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/erkanrzgc/panely/internal/connproto"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// tempSocketPath, unix soketi için yeterince kısa bir geçici yol üretir.
//
// sun_path 108 bayt ile sınırlıdır ve bu sınır aşıldığında hata mesajı
// nedeni açıklamaz. Yol uzunsa testi atlamak, anlamsız bir başarısızlık
// raporlamaktan iyidir.
func tempSocketPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s.sock")
	if len(path) > 100 {
		t.Skipf("geçici yol unix soketi için çok uzun (%d bayt)", len(path))
	}
	return path
}

func listenUnix(t *testing.T, path string) net.Listener {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("unix soketi dinlenemedi (%s): %v", path, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

// TestLocalDialWritesIdentityPreamble, dialLocal'ın gerçek soket üzerinde
// kimlik önsözünü yazdığını doğrular.
func TestLocalDialWritesIdentityPreamble(t *testing.T) {
	path := tempSocketPath(t)
	ln := listenUnix(t, path)

	got := make(chan connproto.Identity, 1)
	failed := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			failed <- err
			return
		}
		defer func() { _ = conn.Close() }()

		id, err := connproto.Read(conn)
		if err != nil {
			failed <- err
			return
		}
		got <- id
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := dialLocal(ctx, path)
	if err != nil {
		t.Fatalf("yerel bağlantı kurulamadı: %v", err)
	}
	defer func() { _ = conn.Close() }()

	select {
	case id := <-got:
		if id.Origin != "local" {
			t.Errorf("origin = %q, beklenen \"local\"", id.Origin)
		}
		if id.Fingerprint != "" {
			t.Errorf("yerel bağlantı parmak izi uydurdu: %q", id.Fingerprint)
		}
	case err := <-failed:
		t.Fatalf("sunucu tarafı hata: %v", err)
	case <-ctx.Done():
		t.Fatal("önsöz hiç gelmedi")
	}
}

// TestLocalDialFailsCleanlyOnMissingSocket, olmayan sokete bağlanmanın
// anlaşılır bir hata verdiğini doğrular.
//
// Hata mesajı soket YOLUNU içermeli: kurulum bozukken operatörün ilk
// sorusu "hangi yola bakıyordu?" oluyor.
func TestLocalDialFailsCleanlyOnMissingSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yok.sock")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := dialLocal(ctx, path)
	if err == nil {
		t.Fatal("olmayan sokete bağlanıldı")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("hata mesajı soket yolunu içermiyor: %v", err)
	}
}

// TestGRPCWorksOverRealSocketAfterPreamble, uçtan uca yolu gerçek unix
// soketi üzerinde doğrular: Dial → önsöz → gRPC.
func TestGRPCWorksOverRealSocketAfterPreamble(t *testing.T) {
	path := tempSocketPath(t)
	ln := listenUnix(t, path)

	creds := &preambleCreds{seen: make(chan connproto.Identity, 1)}
	server := grpc.NewServer(grpc.Creds(creds))
	panelyv1.RegisterPanelyServiceServer(server, &stubService{})

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(ln)
	}()
	t.Cleanup(func() {
		server.Stop()
		<-done
	})

	c, err := Dial(Target{SocketPath: path})
	if err != nil {
		t.Fatalf("istemci kurulamadı: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := c.RPC().Ping(ctx, &panelyv1.PingRequest{ClientVersion: "test"}); err != nil {
		t.Fatalf("gerçek soket üzerinde gRPC çağrısı başarısız: %v", err)
	}

	select {
	case id := <-creds.seen:
		if id.Origin != "local" {
			t.Errorf("sunucunun gördüğü origin = %q", id.Origin)
		}
	default:
		t.Error("sunucu hiç önsöz görmedi")
	}
}

// TestReconnectWritesPreambleAgain, her YENİ bağlantının kendi önsözünü
// aldığını doğrular.
//
// Önsözü Dial() içinde bir kez yazmak ilk bağlantıda çalışır, kopma
// sonrasında sessizce bozulurdu: gRPC kurucuyu yeniden çağırır ama önsöz
// bir daha yazılmazdı. Sunucu burada kasten yeniden başlatılıyor.
func TestReconnectWritesPreambleAgain(t *testing.T) {
	path := tempSocketPath(t)
	seen := make(chan connproto.Identity, 8)

	serve := func() (*grpc.Server, chan struct{}) {
		ln, err := net.Listen("unix", path)
		if err != nil {
			t.Fatalf("unix soketi dinlenemedi: %v", err)
		}
		server := grpc.NewServer(grpc.Creds(&preambleCreds{seen: seen}))
		panelyv1.RegisterPanelyServiceServer(server, &stubService{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = server.Serve(ln)
		}()
		return server, done
	}

	server, done := serve()

	c, err := Dial(Target{SocketPath: path})
	if err != nil {
		t.Fatalf("istemci kurulamadı: %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	if _, err := c.RPC().Ping(ctx, &panelyv1.PingRequest{}); err != nil {
		t.Fatalf("ilk çağrı başarısız: %v", err)
	}

	// Sunucuyu düşür: istemcinin bağlantısı kopar.
	server.Stop()
	<-done
	_ = os.Remove(path)

	server2, done2 := serve()
	t.Cleanup(func() {
		server2.Stop()
		<-done2
	})

	// Önsöz yeniden yazılmazsa el sıkışma başarısız olur ve bu döngü
	// süre sınırına kadar hata verir.
	var lastErr error
	for range 40 {
		callCtx, callCancel := context.WithTimeout(ctx, 2*time.Second)
		_, lastErr = c.RPC().Ping(callCtx, &panelyv1.PingRequest{})
		callCancel()
		if lastErr == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("yeniden bağlanma zaman aşımı, son hata: %v", lastErr)
		case <-time.After(250 * time.Millisecond):
		}
	}
	if lastErr != nil {
		t.Fatalf("yeniden bağlandıktan sonra çağrı başarısız: %v", lastErr)
	}

	if len(seen) < 2 {
		t.Errorf("sunucunun gördüğü önsöz sayısı = %d, en az 2 bekleniyordu "+
			"(ilk bağlantı + yeniden bağlanma)", len(seen))
	}
}

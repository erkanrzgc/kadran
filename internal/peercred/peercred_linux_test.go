//go:build linux

package peercred

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// dialPair, gerçek bir unix soketi üzerinden bağlı bir çift bağlantı kurar.
func dialPair(t *testing.T) (server, client net.Conn) {
	t.Helper()

	sockPath := filepath.Join(t.TempDir(), "test.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("dinleyici açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	type result struct {
		conn net.Conn
		err  error
	}
	accepted := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		accepted <- result{c, err}
	}()

	client, err = net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("bağlanılamadı: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })

	select {
	case r := <-accepted:
		if r.err != nil {
			t.Fatalf("bağlantı kabul edilemedi: %v", r.err)
		}
		t.Cleanup(func() { _ = r.conn.Close() })
		return r.conn, client
	case <-time.After(5 * time.Second):
		t.Fatal("bağlantı kabulü zaman aşımına uğradı")
		return nil, nil
	}
}

func TestFromConnReportsCurrentProcess(t *testing.T) {
	server, _ := dialPair(t)

	cred, err := FromConn(server)
	if err != nil {
		t.Fatalf("kimlik okunamadı: %v", err)
	}

	if got, want := cred.UID, uint32(os.Getuid()); got != want {
		t.Errorf("uid = %d, beklenen %d", got, want)
	}
	if got, want := cred.GID, uint32(os.Getgid()); got != want {
		t.Errorf("gid = %d, beklenen %d", got, want)
	}
	if got, want := cred.PID, int32(os.Getpid()); got != want {
		t.Errorf("pid = %d, beklenen %d", got, want)
	}
}

func TestFromConnRejectsNonUnixConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp dinleyici açılamadı: %v", err)
	}
	defer func() { _ = ln.Close() }()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("tcp bağlantısı kurulamadı: %v", err)
	}
	defer func() { _ = client.Close() }()

	if _, err := FromConn(client); !errors.Is(err, ErrNotUnixConn) {
		t.Errorf("TCP bağlantısı için ErrNotUnixConn bekleniyordu, %v alındı", err)
	}
}

func TestServerHandshakeAcceptsAllowedCaller(t *testing.T) {
	server, _ := dialPair(t)

	tc, err := TransportCredentials(Policy{AllowUIDs: []uint32{uint32(os.Getuid())}})
	if err != nil {
		t.Fatalf("oluşturulamadı: %v", err)
	}

	conn, info, err := tc.ServerHandshake(server)
	if err != nil {
		t.Fatalf("izinli çağıran reddedildi: %v", err)
	}
	if conn == nil {
		t.Error("bağlantı döndürülmedi")
	}

	auth, ok := info.(AuthInfo)
	if !ok {
		t.Fatalf("AuthInfo tipi beklenmedik: %T", info)
	}
	if auth.Cred.UID != uint32(os.Getuid()) {
		t.Errorf("AuthInfo yanlış uid taşıyor: %d", auth.Cred.UID)
	}
	if auth.AuthType() != "peercred" {
		t.Errorf("AuthType = %q", auth.AuthType())
	}
}

// TestServerHandshakeRejectsDisallowedCaller, güvenlik sınırının asıl
// testidir: politikaya uymayan çağıran tek bir RPC baytı bile
// gönderemeden reddedilmeli ve bağlantı kapatılmalıdır.
func TestServerHandshakeRejectsDisallowedCaller(t *testing.T) {
	server, client := dialPair(t)

	// Mevcut süreçten kesinlikle farklı bir uid.
	forbidden := uint32(os.Getuid()) + 1
	tc, err := TransportCredentials(Policy{AllowUIDs: []uint32{forbidden}})
	if err != nil {
		t.Fatalf("oluşturulamadı: %v", err)
	}

	if _, _, err := tc.ServerHandshake(server); !errors.Is(err, ErrDenied) {
		t.Fatalf("izinsiz çağıran için ErrDenied bekleniyordu, %v alındı", err)
	}

	// Reddedilen bağlantı kapatılmış olmalı: istemcinin okuması EOF ile
	// veya hatayla dönmeli, asla veri beklememeli.
	if err := client.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatalf("okuma süresi ayarlanamadı: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := client.Read(buf); err == nil {
		t.Error("reddedilen bağlantı kapatılmadı")
	}
}

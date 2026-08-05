package client

import (
	"context"
	"io"
	"os"
	"testing"
	"time"
)

// # Bu dosya neden var?
//
// `panely status <kullanıcı>@<sunucu>` gerçek sunucuda şu hatayı
// veriyordu:
//
//	rpc error: code = Unavailable desc = connection error:
//	desc = "error reading server preface: EOF"
//
// Daemon sağlamdı: aynı sunucuda panely-client kullanıcısı yerel sokete
// bağlanıp tam çıktı alıyordu. Kopan tek şey SSH taşımasıydı.
//
// Neden: `dialSSH`, ssh alt sürecini `exec.CommandContext(ctx, ...)` ile
// başlatıyordu ve buradaki ctx gRPC'nin BAĞLANTI DENEMESİ bağlamı.
// gRPC o bağlamı el sıkışma biter bitmez iptal eder; CommandContext de
// iptalde süreci ÖLDÜRÜR. Yani ssh, bağlantı kurulur kurulmaz ölüyordu.
//
// Yerel yolun çalışması da bunu doğruluyordu: unix soketine bağlandıktan
// sonra bağlamın iptali bağlantıyı etkilemez.
//
// Birim testleri bunu göremezdi: hepsi `dialSSH`'ı iptal edilmeyen bir
// bağlamla çağırıyordu.

// TestSSHProcessSurvivesDialContextCancel, bağlantı kurulduktan SONRA
// dial bağlamının iptal edilmesinin taşımayı ÖLDÜRMEDİĞİNİ doğrular.
//
// Alt sürecin ömrü bağlantıya bağlı olmalı, tek bir bağlantı denemesine
// değil.
func TestSSHProcessSurvivesDialContextCancel(t *testing.T) {
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}

	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHEchoEnv, "1")

	original := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = original })

	ctx, cancel := context.WithCancel(context.Background())
	conn, err := dialSSH(ctx, Target{SSHUser: "panely-client", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("bağlantı kurulamadı: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// gRPC'nin yaptığı tam olarak bu: deneme bitince bağlamı iptal eder.
	cancel()
	time.Sleep(200 * time.Millisecond)

	mesaj := []byte("panely-canli\n")
	if _, err := conn.Write(mesaj); err != nil {
		t.Fatalf("iptalden sonra yazılamadı — ssh süreci öldürülmüş: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	alinan := make([]byte, len(mesaj))
	if _, err := io.ReadFull(conn, alinan); err != nil {
		t.Fatalf("iptalden sonra okunamadı (%v) — taşıma öldü. "+
			"Gerçek sunucuda görünümü: "+
			"\"error reading server preface: EOF\"", err)
	}
	if string(alinan) != string(mesaj) {
		t.Errorf("yankı %q, beklenen %q", alinan, mesaj)
	}
}

// TestSSHProcessDiesWhenConnectionCloses, sürecin ömrünün BAĞLANTIYA
// bağlı olduğunu doğrular.
//
// Yukarıdaki testi "süreci hiç öldürme" diyerek de geçmek mümkündü; o
// zaman her bağlantı arkada asılı bir ssh bırakırdı. Bu satır o kaçışı
// kapatıyor.
func TestSSHProcessDiesWhenConnectionCloses(t *testing.T) {
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}

	t.Setenv(fakeSSHEnv, "1")
	t.Setenv(fakeSSHEchoEnv, "1")

	original := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = original })

	conn, err := dialSSH(context.Background(),
		Target{SSHUser: "panely-client", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("bağlantı kurulamadı: %v", err)
	}

	// Close, alt sürecin toplanmasını beklemeli. Süreç asılı kalırsa
	// bu çağrı süresiz bloklar; testin zaman aşımı onu yakalar.
	bitti := make(chan error, 1)
	go func() { bitti <- conn.Close() }()

	select {
	case <-bitti:
		// Toplandı.
	case <-time.After(10 * time.Second):
		t.Fatal("Close() döndü değil — ssh süreci toplanmıyor, " +
			"her bağlantı arkada asılı bir süreç bırakır")
	}
}

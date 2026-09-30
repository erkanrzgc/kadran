package client

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// # Bu dosya neden var?
//
// ssh bağlanamadığında (yanlış sunucu adı, reddedilen anahtar, DEĞİŞMİŞ
// host anahtarı) kullanıcı yalnızca şunu görüyordu:
//
//	rpc error: code = Unavailable desc = connection error:
//	desc = "error reading server preface: EOF"
//
// ssh'ın asıl mesajı yakalanıyordu, ama yalnızca bağlantı KAPANIRKEN
// okunuyordu; gRPC o ana kadar EOF'u çoktan kendi mesajına çevirmiş
// oluyordu. 30 Eylül'de gerçek CLI'da ölçüldü: `panely status
// panely-client@yok-boyle-bir-sunucu.invalid` bu mesajı verdi (K-120).
//
// "Host key verification failed" de aynı yoldan kayboluyordu. Kullanıcı
// sunucunun kimliği değişti uyarısını değil, anlaşılmaz bir gRPC hatası
// görüyordu.

const reddedildi = "panely-client@sunucu: Permission denied (publickey)."

func sahteSSH(t *testing.T, env map[string]string) {
	t.Helper()
	if _, err := os.Stat(os.Args[0]); err != nil {
		t.Skipf("test binary'si bulunamadı, sahte ssh kurulamıyor: %v", err)
	}
	t.Setenv(fakeSSHEnv, "1")
	for k, v := range env {
		t.Setenv(k, v)
	}
	original := sshCommand
	sshCommand = os.Args[0]
	t.Cleanup(func() { sshCommand = original })
}

// TestSSHFailureReachesTheReader: taşıma katmanında, stdout kapanınca
// okuyucu düz EOF değil ssh'ın kendi mesajını almalı.
func TestSSHFailureReachesTheReader(t *testing.T) {
	sahteSSH(t, map[string]string{fakeSSHFailEnv: reddedildi})

	conn, err := dialSSH(context.Background(), Target{SSHUser: "panely-client", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("ssh başlatılamadı: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	_, err = conn.Read(make([]byte, 16))
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("okuyucu ssh'ın sebebini almadı: %v", err)
	}
}

// TestSSHFailureReachesTheUser: kullanıcının gördüğü yol. CLI'ın
// kullandığı Dial + gerçek bir RPC; hata metninde ssh'ın sebebi olmalı.
func TestSSHFailureReachesTheUser(t *testing.T) {
	sahteSSH(t, map[string]string{fakeSSHFailEnv: reddedildi})

	c, err := Dial(Target{SSHUser: "panely-client", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("istemci kurulamadı: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = c.CheckProtocol(ctx)
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("kullanıcı ssh'ın sebebini görmüyor: %v", err)
	}
}

// TestSSHFailureIsBoundedWhenAChildHoldsStderr: ssh ölüp stderr'i açık
// tutan bir alt süreç bırakırsa (ControlPersist ustası gibi), cmd.Wait
// stderr kopyasını o süreç bitene kadar bekler. Okuyucu EOF'ta Wait'i
// beklediği için hızlı bir ssh hatası uzun bir asılmaya dönerdi.
// cmd.WaitDelay bunu sınırlıyor; mesaj yine ulaşmalı.
func TestSSHFailureIsBoundedWhenAChildHoldsStderr(t *testing.T) {
	pidDosyasi := filepath.Join(t.TempDir(), "torun.pid")
	sahteSSH(t, map[string]string{fakeSSHLingerEnv: reddedildi, fakeSSHLingerPIDEnv: pidDosyasi})
	t.Cleanup(func() {
		if b, err := os.ReadFile(pidDosyasi); err == nil {
			if pid, err := strconv.Atoi(string(b)); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})

	conn, err := dialSSH(context.Background(), Target{SSHUser: "panely-client", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("ssh başlatılamadı: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	basla := time.Now()
	_, err = conn.Read(make([]byte, 16))
	if gecen := time.Since(basla); gecen > sshExitGrace+5*time.Second {
		t.Fatalf("okuyucu %s bekledi — stderr'i tutan alt süreç Wait'i kilitliyor", gecen.Round(time.Second))
	}
	if err == nil || !strings.Contains(err.Error(), "Permission denied") {
		t.Fatalf("okuyucu ssh'ın sebebini almadı: %v", err)
	}
}

// TestCleanSSHExitIsPlainEOF, kontrol grubu: ssh sessizce ve başarıyla
// çıkarsa okuyucu yine düz io.EOF almalı. Yoksa her normal kapanış bir
// hata gibi görünürdü.
func TestCleanSSHExitIsPlainEOF(t *testing.T) {
	sahteSSH(t, map[string]string{fakeSSHCleanEnv: "1"})

	conn, err := dialSSH(context.Background(), Target{SSHUser: "panely-client", SSHHost: "sunucu"})
	if err != nil {
		t.Fatalf("ssh başlatılamadı: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.Read(make([]byte, 16)); !errors.Is(err, io.EOF) {
		t.Fatalf("temiz çıkışta okuyucu %v aldı, beklenen io.EOF", err)
	}
}

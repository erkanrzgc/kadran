package api

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc/credentials"

	"github.com/erkanrzgc/panely/internal/peercred"
)

// retEdenPeer, iç el sıkışmayı peercred'in GERÇEK ret hatasıyla
// düşüren sahte bir taşıma kimlik bilgisidir.
type retEdenPeer struct {
	credentials.TransportCredentials
	cred peercred.Cred
}

func (r retEdenPeer) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	// peercred.ServerHandshake'in ürettiği biçimin aynısı.
	return nil, nil, fmt.Errorf("%w: %s", peercred.ErrDenied, r.cred)
}

func (r retEdenPeer) Clone() credentials.TransportCredentials { return r }

// logYakala, testin süresince varsayılan slog'u bir tampona yönlendirir.
func logYakala(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	onceki := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(onceki) })
	return &buf
}

// TestRejectedCallerIsLogged, SO_PEERCRED reddinin günlüğe KİMLİKLE
// birlikte yazıldığını doğrular.
//
// ── Neden var (K-095) ────────────────────────────────────────────────
//
// root ve panely api.sock'a bağlanınca reddediliyordu — doğru davranış —
// ama journal'da TEK SATIR yoktu. gRPC el sıkışma hatalarını kendi
// günlükçüsüne yazıyor ve o günlükçü varsayılan olarak sessiz. İstemci
// tarafı yalnızca "connection reset by peer" görüyor.
//
// Sonuç: `usermod -aG panely-client` ile ikinci bir yönetici ekleyen
// operatör (SECURITY.md: SO_PEERCRED yalnızca BİRİNCİL grubu raporlar)
// sunucu tarafında hiçbir iz bulamaz. Teşhis edilemeyen bir ret, yanlış
// alarm kadar güven yakar.
func TestRejectedCallerIsLogged(t *testing.T) {
	buf := logYakala(t)
	creds := &callerCreds{peer: retEdenPeer{cred: peercred.Cred{PID: 4242, UID: 0, GID: 0}}}

	istemci, sunucu := net.Pipe()
	t.Cleanup(func() { _ = istemci.Close(); _ = sunucu.Close() })

	_, _, err := creds.ServerHandshake(sunucu)
	if !errors.Is(err, peercred.ErrDenied) {
		t.Fatalf("hata = %v, peercred.ErrDenied bekleniyordu — ret yolu sınanmıyor", err)
	}

	cikti := buf.String()
	if !strings.Contains(cikti, "reddedildi") {
		t.Fatalf("ret günlüğe yazılmadı — operatör sunucu tarafında hiçbir iz bulamaz:\n%q", cikti)
	}
	for _, alan := range []string{"uid=0", "gid=0", "pid=4242"} {
		if !strings.Contains(cikti, alan) {
			t.Errorf("ret satırında %s yok — KİMİN reddedildiği görünmüyor:\n%s", alan, cikti)
		}
	}
}

// TestAcceptedCallerIsNotLoggedAsRejected, kabul edilen bağlantının ret
// satırı ÜRETMEDİĞİNİ doğrular — kontrol grubu.
//
// Her bağlantıda "reddedildi" yazan bir günlük, ret satırını anlamsız
// kılardı: gerçek ret gürültüde kaybolur.
func TestAcceptedCallerIsNotLoggedAsRejected(t *testing.T) {
	buf := logYakala(t)
	creds := &callerCreds{peer: kabulEdenPeer{}}

	istemci, sunucu := net.Pipe()
	t.Cleanup(func() { _ = istemci.Close(); _ = sunucu.Close() })

	// Kabul edilen çağıran önsöz göndermezse el sıkışma önsöz
	// aşamasında düşer; burada yalnızca peercred aşamasının ret satırı
	// üretmediği sınanıyor, o yüzden bağlantı hemen kapatılıyor.
	_ = istemci.Close()
	_, _, _ = creds.ServerHandshake(sunucu)

	if strings.Contains(buf.String(), peercred.ErrDenied.Error()) {
		t.Errorf("kabul edilen çağıran peercred reddi olarak günlüğe yazıldı:\n%s", buf.String())
	}
}

type kabulEdenPeer struct {
	credentials.TransportCredentials
}

func (kabulEdenPeer) ServerHandshake(c net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return c, peercred.AuthInfo{Cred: peercred.Cred{UID: 996, GID: 987}}, nil
}

func (k kabulEdenPeer) Clone() credentials.TransportCredentials { return k }

// panely-connect, SSH oturumu ile panelyd'nin unix soketi arasında bayt
// taşır.
//
// authorized_keys'te zorlanmış komut (forced command) olarak çalışır:
//
//	command="/usr/local/lib/panely/panely-connect",restrict ssh-ed25519 AAAA...
//
// `restrict` her şeyi kapatır: pty yok, port yönlendirme yok, ajan
// yönlendirme yok, X11 yok, user-rc yok. Zorlanmış komut da istemcinin
// kendi komutunu çalıştırmasını engeller. Bu anahtarla yapılabilecek tek
// şey bu programı çalıştırmaktır.
//
// # Neden soket yönlendirmesi değil?
//
// OpenSSH'ta unix soketi yönlendirmesini açmak `port-forwarding` iznini
// gerektirir; bu da istemciye sunucudaki HER TCP portuna tünel açma yetkisi
// verirdi — örneğin localhost:5432'deki veritabanına. Zorlanmış komut bu
// sınıfı tamamen kapatır (docs/decisions.md K-003).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/erkanrzgc/panely/internal/connproto"
	"github.com/erkanrzgc/panely/internal/sshenv"
	"github.com/erkanrzgc/panely/internal/version"
)

const (
	defaultSocket = "/run/panely/api.sock"

	// dialTimeout, panelyd soketine bağlanmak için tanınan süre.
	dialTimeout = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		// TÜM tanılama stderr'e gider. stdout protokol kanalıdır;
		// oraya yazılan tek bir bayt gRPC akışını bozar.
		fmt.Fprintln(os.Stderr, "panely-connect:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		socketPath  = flag.String("socket", defaultSocket, "panelyd api soketi")
		showVersion = flag.Bool("version", false, "sürümü yazdır ve çık")
	)
	flag.Parse()

	if *showVersion {
		fmt.Fprintf(os.Stderr, "panely-connect %s (%s) protokol %d\n",
			version.Version, version.Commit, version.Protocol)
		return nil
	}

	// SSH_ORIGINAL_COMMAND, istemcinin çalıştırmak İSTEDİĞİ komutu taşır.
	// Kasıtlı olarak YOK SAYILIR — zorlanmış komutun tüm amacı budur.
	// Okumak bile istemiyoruz ki ileride kazara kullanılmasın.

	identity := resolveIdentity()

	// # Neden zaman aşımlı bağlanılıyor?
	//
	// Bu süreç bir SSH oturumunun içinde çalışıyor. panelyd yanıt
	// vermiyorsa (askıda, yeniden başlıyor, soket dosyası duruyor ama
	// arkasında kimse yok) zaman aşımsız bir Dial süresiz bekler ve
	// SSH oturumunu açık tutar. Bağlantı başına bir asılı süreç,
	// birikince sunucuda gerçek bir kaynak sorunu.
	//
	// Yerel unix soketine bağlanmak milisaniyeler sürer; 10 saniye
	// fazlasıyla cömert bir üst sınır.
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", *socketPath)
	if err != nil {
		return fmt.Errorf("panelyd'ye bağlanılamadı (%s): %w", *socketPath, err)
	}
	defer func() { _ = conn.Close() }()

	// Kimlik önsözü, uzak istemciden TEK BAYT okumadan önce yazılır.
	// Sıralama güvenlik açısından kritiktir: istemcinin baytları ondan
	// sonra geldiği için kimliği değiştiremez (bkz. internal/connproto).
	if err := connproto.Write(conn, identity); err != nil {
		return err
	}

	return pump(conn)
}

// resolveIdentity, sshd'nin ortam değişkenlerinden çağıranın kimliğini
// türetir.
//
// SSH ortamı yoksa (sunucuda elle çalıştırma) "local" kökenli, kimliksiz
// bir kayıt üretilir. Uydurma bir kimlik yazmaktansa "bilinmiyor" demek
// denetim izini dürüst tutar.
func resolveIdentity() connproto.Identity {
	sshID, err := sshenv.Parse(os.Getenv)
	if err != nil {
		return connproto.Identity{Origin: "local"}
	}
	return connproto.Identity{
		Fingerprint: sshID.Fingerprint,
		SourceIP:    sshID.SourceIP,
		Origin:      "ssh",
	}
}

// pump, stdin/stdout ile soket arasında çift yönlü bayt taşır.
//
// # Yarım kapanış neden önemli?
//
// İstemci isteğini gönderip yazmayı bitirdiğinde stdin EOF verir. Bu
// noktada süreçten çıkmak YANLIŞ olurdu: sunucunun yanıtı hâlâ yolda
// olabilir. Doğru davranış, sokete "artık yazmayacağım" demek
// (CloseWrite) ve yanıt akışını sonuna kadar taşımaya devam etmektir.
//
// Oturumun bittiği yer soket→stdout yönüdür; süreç orada sonlanır.
func pump(conn net.Conn) error {
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("beklenmedik bağlantı türü: %T", conn)
	}

	upstream := make(chan error, 1)
	go func() {
		_, err := io.Copy(unixConn, os.Stdin)
		// Yarım kapanış: sunucu isteğin bittiğini görsün.
		_ = unixConn.CloseWrite()
		upstream <- err
	}()

	// Soket→stdout yönü oturumun ömrünü belirler.
	if _, err := io.Copy(os.Stdout, unixConn); err != nil && !isExpectedClose(err) {
		return fmt.Errorf("yanıt akışı kesildi: %w", err)
	}

	// Yukarı akışta biriken bir hata varsa bildir; yoksa bekleme.
	select {
	case err := <-upstream:
		if err != nil && !isExpectedClose(err) {
			return fmt.Errorf("istek akışı kesildi: %w", err)
		}
	default:
	}
	return nil
}

// isExpectedClose, oturum sonu sayılan hataları tanır.
func isExpectedClose(err error) bool {
	return errors.Is(err, io.EOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrClosed)
}

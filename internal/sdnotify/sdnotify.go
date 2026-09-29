// Package sdnotify, systemd'nin hazır-olma protokolünü uygular.
//
// Type=notify unit dosyaları servisin "hazırım" demesini bekler. Bunun
// alternatifi Type=simple'dır, ama o durumda systemd süreç doğar doğmaz
// servisi hazır sayar — oysa panelyd o anda henüz veritabanını açmamış,
// göçleri uygulamamış ve executor'a bağlanmamıştır. Bağımlı servisler ve
// `panely bootstrap`'in sağlık kontrolü yanlış zamanda yeşil görürdü.
//
// Protokol tek satırlık bir datagram olduğu için harici bağımlılık
// gerekmez; go-systemd paketini çekmek bu kadarı için orantısızdır.
package sdnotify

import (
	"errors"
	"net"
	"os"
)

// ErrNoSocket, NOTIFY_SOCKET tanımlı olmadığında döner. Bu bir hata
// durumu değildir: servis systemd dışında (testte, elle) çalışıyordur.
var ErrNoSocket = errors.New("sdnotify: NOTIFY_SOCKET tanımlı değil")

// Ready, systemd'ye servisin hazır olduğunu bildirir.
func Ready() error { return Send("READY=1") }

// Stopping, systemd'ye kapanışın başladığını bildirir.
func Stopping() error { return Send("STOPPING=1") }

// Status, systemd'ye insan tarafından okunabilir bir durum metni bildirir.
// `systemctl status panelyd` çıktısında görünür.
func Status(text string) error { return Send("STATUS=" + text) }

// Send, systemd'ye ham bir bildirim satırı gönderir.
//
// Dışa açık, çünkü panelyd'nin watchdog'u WATCHDOG=1 gönderiyor
// (internal/liveness, K-115). Onun için ayrı bir `Watchdog()` yazmak,
// executor'ın hiç çağırmadığı bir fonksiyonu ayrıcalıklı yüzeye eklerdi;
// bu paket panely-exec'in bağımlılığı ve bütçe 2498/2500.
func Send(payload string) error {
	addr := os.Getenv("NOTIFY_SOCKET")
	if addr == "" {
		return ErrNoSocket
	}

	// '@' ile başlayan yol soyut ad alanını (abstract namespace) belirtir
	// ve çekirdeğe NUL baytıyla iletilir.
	if addr[0] == '@' {
		addr = "\x00" + addr[1:]
	}

	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	_, err = conn.Write([]byte(payload))
	return err
}

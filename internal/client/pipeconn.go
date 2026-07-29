package client

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// pipeConn, bir okuyucu/yazıcı çiftini net.Conn arayüzüne uydurur.
//
// Panely'de istemci sunucuya SSH üzerinden bağlanır: `ssh -T user@host`
// alt süreç olarak çalıştırılır ve onun stdin/stdout boruları gRPC'nin
// taşıması olur. Sunucu tarafında sshd bu boruları zorlanmış komuta
// (panely-connect) bağlar, o da api.sock'a taşır.
//
// Böylece gRPC uçtan uca çalışır ve arada hiçbir yerde açık bir ağ portu
// bulunmaz — şartnamedeki "gerçek IP'yi gizle" gereksinimi, gizlenecek bir
// port olmadığı için kendiliğinden karşılanır.
type pipeConn struct {
	reader io.ReadCloser
	writer io.WriteCloser

	// cleanup, bağlantı kapandığında alt sürecin toplanmasını sağlar.
	cleanup func() error

	closeOnce sync.Once
	closeErr  error

	local  pipeAddr
	remote pipeAddr
}

type pipeAddr struct {
	network string
	address string
}

func (a pipeAddr) Network() string { return a.network }
func (a pipeAddr) String() string  { return a.address }

// newPipeConn, verilen boru çiftinden bir net.Conn üretir.
func newPipeConn(r io.ReadCloser, w io.WriteCloser, remote string, cleanup func() error) *pipeConn {
	return &pipeConn{
		reader:  r,
		writer:  w,
		cleanup: cleanup,
		local:   pipeAddr{network: "pipe", address: "local"},
		remote:  pipeAddr{network: "pipe", address: remote},
	}
}

func (c *pipeConn) Read(p []byte) (int, error)  { return c.reader.Read(p) }
func (c *pipeConn) Write(p []byte) (int, error) { return c.writer.Write(p) }

// Close, boruları kapatır ve alt süreci toplar.
//
// Yazma ucu ÖNCE kapatılır: karşı taraf EOF görüp temiz kapanabilsin.
// Ters sırada, okuma ucunu önce kapatmak karşı tarafı yazarken kırık boru
// hatasıyla baş başa bırakırdı.
func (c *pipeConn) Close() error {
	c.closeOnce.Do(func() {
		var errs []error
		if err := c.writer.Close(); err != nil {
			errs = append(errs, err)
		}
		if err := c.reader.Close(); err != nil {
			errs = append(errs, err)
		}
		if c.cleanup != nil {
			if err := c.cleanup(); err != nil {
				errs = append(errs, err)
			}
		}
		c.closeErr = errors.Join(errs...)
	})
	return c.closeErr
}

func (c *pipeConn) LocalAddr() net.Addr  { return c.local }
func (c *pipeConn) RemoteAddr() net.Addr { return c.remote }

// SetDeadline ve türevleri desteklenmez.
//
// Alt süreç boruları — özellikle Windows'ta — süre sınırı kabul etmez.
// net.Conn arayüzü bu metotları zorunlu kıldığı için uygulanmaları gerekiyor.
//
// SESSİZCE BAŞARILI DÖNMÜYORUZ. Çağıran bir süre sınırı koyduğunu sanıp
// koyamamışsa, bir hata durumunda sonsuza kadar bekleyebilirdi. Açıkça
// os.ErrNoDeadline döndürmek, çağıranın bunu fark etmesini sağlar.
//
// gRPC bu durumu doğru ele alıyor: RPC süre sınırlarını context üzerinden
// yönetiyor ve bağlantı süre sınırlarına bel bağlamıyor. Bu varsayılmadı,
// ÖLÇÜLDÜ — bkz. TestGRPCWorksOverPipeConn.
func (c *pipeConn) SetDeadline(time.Time) error      { return errNoDeadline }
func (c *pipeConn) SetReadDeadline(time.Time) error  { return errNoDeadline }
func (c *pipeConn) SetWriteDeadline(time.Time) error { return errNoDeadline }

var errNoDeadline = errors.New("client: boru bağlantısı süre sınırı desteklemiyor")

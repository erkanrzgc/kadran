package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/erkanrzgc/panely/internal/connproto"
	"github.com/erkanrzgc/panely/internal/peercred"
)

// PreambleTimeout, çağıranın kimlik önsözünü göndermesi için tanınan süre.
//
// Bağlanıp hiç veri göndermeyen bir süreç, süresiz açık kalan bir bağlantı
// biriktirebilir. Yerel unix soketinde bir saniye fazlasıyla yeterlidir.
const PreambleTimeout = 3 * time.Second

// CallerInfo, doğrulanmış çağıran kimliğini handler'lara taşır.
//
// İki bağımsız kaynağı birleştirir:
//
//   - Unix: çekirdekten SO_PEERCRED ile alınır, uydurulamaz.
//   - Identity: panely-connect tarafından, sshd'nin ortam değişkenlerinden
//     türetilerek yazılır. Uzak istemci bunu belirleyemez (bkz. connproto).
type CallerInfo struct {
	Unix     peercred.Cred
	Identity connproto.Identity
}

// AuthType, credentials.AuthInfo arayüzünü karşılar.
func (CallerInfo) AuthType() string { return "panely-caller" }

// TransportCredentials, api.sock için taşıma kimlik bilgisi üretir.
//
// El sıkışma sırası kasıtlıdır:
//
//  1. ÖNCE SO_PEERCRED. Çekirdek doğrulaması en ucuz ve en güçlü kontrol;
//     yetkisiz bir çağıran tek bayt göndermeden reddedilir.
//  2. SONRA önsöz. Yalnızca yetkili bir çağıranın yazdığı veri okunur.
//
// Ters sırada olsaydı, yetkisiz bir süreç bize 4 KB'a kadar veri
// gönderebilir ve reddedilmeden önce ayrıştırma kodumuzu çalıştırabilirdi.
func TransportCredentials(clientGID uint32) (credentials.TransportCredentials, error) {
	inner, err := peercred.TransportCredentials(peercred.Policy{
		AllowGIDs: []uint32{clientGID},
	})
	if err != nil {
		return nil, err
	}
	return &callerCreds{peer: inner}, nil
}

type callerCreds struct {
	peer credentials.TransportCredentials
}

// ServerHandshake, el sıkışmayı yürütür ve BAŞARISIZLIĞI günlüğe yazar.
//
// ── Ret SESSİZ olmamalı (K-095) ──────────────────────────────────────
//
// gRPC el sıkışma hatalarını kendi günlükçüsüne yazıyor ve o günlükçü
// varsayılan olarak sessiz. Yani root ya da yanlış gruptaki bir
// kullanıcı reddedildiğinde istemci "connection reset by peer" görüyor,
// sunucu journal'ında TEK SATIR yok. `usermod -aG panely-client` ile
// eklenen ikinci bir yönetici (SO_PEERCRED yalnızca birincil grubu
// raporlar) sorununu sunucu tarafında hiç bulamazdı.
//
// Hata metni peercred'den gelir ve `pid= uid= gid=` taşır; KİMİN
// reddedildiği böylece görünür. Soketin dizini 0750 panely:panely-client
// olduğundan buraya ulaşabilen herkes zaten yerel ve ayrıcalıklıdır;
// satırları çoğaltarak günlüğü boğma riski ihmal edilebilir.
func (c *callerCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := c.handshake(raw)
	if err != nil {
		slog.Warn("api.sock bağlantısı reddedildi", "sebep", err)
	}
	return conn, info, err
}

func (c *callerCreds) handshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, peerInfo, err := c.peer.ServerHandshake(raw)
	if err != nil {
		return nil, nil, err
	}

	unixCred, ok := peerInfo.(peercred.AuthInfo)
	if !ok {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("api: beklenmedik peercred bilgisi: %T", peerInfo)
	}

	// Önsöz okuması bir zaman aşımıyla sınırlanır, sonra kaldırılır:
	// gRPC akışının kendi zamanlaması bizim koyduğumuz süreye takılmamalı.
	if err := conn.SetReadDeadline(time.Now().Add(PreambleTimeout)); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("api: okuma süresi ayarlanamadı: %w", err)
	}

	identity, err := connproto.Read(conn)
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("api: kimlik önsözü alınamadı: %w", err)
	}

	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("api: okuma süresi sıfırlanamadı: %w", err)
	}

	return conn, CallerInfo{Unix: unixCred.Cred, Identity: identity}, nil
}

func (c *callerCreds) ClientHandshake(context.Context, string, net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, errors.New("api: istemci tarafında kullanılamaz")
}

func (c *callerCreds) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "panely-caller"}
}

func (c *callerCreds) Clone() credentials.TransportCredentials {
	return &callerCreds{peer: c.peer.Clone()}
}

func (c *callerCreds) OverrideServerName(string) error {
	return errors.New("api: sunucu adı geçersiz kılınamaz")
}

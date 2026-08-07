// Package execclient, panelyd'nin ayrıcalıklı executor'a bağlanmasını
// sağlar.
package execclient

import (
	"context"
	"fmt"
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/erkanrzgc/panely/internal/audit"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/version"
)

// Client, executor'a bağlı bir gRPC istemcisidir.
type Client struct {
	conn *grpc.ClientConn
	rpc  panelyv1.ExecutorServiceClient
}

// Dial, executor soketine bağlanır.
//
// # Neden insecure taşıma?
//
// "insecure" burada TLS'siz demektir, korumasız değil. Güven sınırı
// dosya sistemidir: sokete ancak /run/panely-exec dizinini traverse
// edebilen (0750 root:panely) bir süreç ulaşabilir, ve ulaştığında da
// sunucu tarafı SO_PEERCRED ile uid'ini doğrular. Aynı makinedeki bir
// unix soketine TLS eklemek, korumayı artırmadan anahtar yönetimi
// yükü getirirdi.
//
// # Neden passthrough:/// ve özel dialer?
//
// gRPC'nin varsayılan çözümleyicisini tamamen devreden çıkarır. Ölçümler
// `unix://` hedefinin de sertleştirilmiş systemd kısıtları altında
// çalıştığını gösterdi (docs/decisions.md K-009), ama çözümleyiciyi hiç
// devreye sokmamak bir bilinmeyeni kalıcı olarak siler ve hiçbir şeye
// mal olmaz.
func Dial(socketPath string) (*Client, error) {
	conn, err := grpc.NewClient(
		"passthrough:///"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", addr)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("executor'a bağlanılamadı: %w", err)
	}
	return &Client{conn: conn, rpc: panelyv1.NewExecutorServiceClient(conn)}, nil
}

// Close, bağlantıyı kapatır.
func (c *Client) Close() error { return c.conn.Close() }

// PingResult, executor'ın kimlik ve sürüm bilgisidir.
type PingResult struct {
	Version         string
	ProtocolVersion uint32
	EffectiveUID    uint32
}

// Ping, executor'ın canlılığını sorgular ve sürüm uyumunu denetler.
func (c *Client) Ping(ctx context.Context) (PingResult, error) {
	resp, err := c.rpc.Ping(ctx, &panelyv1.ExecutorServicePingRequest{})
	if err != nil {
		return PingResult{}, fmt.Errorf("executor yanıt vermiyor: %w", err)
	}

	if resp.GetProtocolVersion() != version.Protocol {
		return PingResult{}, fmt.Errorf(
			"protokol uyumsuzluğu: daemon %d, executor %d — ikisi birlikte güncellenmeli",
			version.Protocol, resp.GetProtocolVersion())
	}

	// Executor ayrıcalıklı çalışmıyorsa Docker'a erişemez ve her işlem
	// anlaşılmaz hatalarla başarısız olur. Burada yakalamak, sonradan
	// teşhis etmekten çok daha ucuz.
	if resp.GetEffectiveUid() != 0 {
		return PingResult{}, fmt.Errorf(
			"executor root çalışmıyor (uid %d) — systemd unit dosyası bozulmuş olabilir",
			resp.GetEffectiveUid())
	}

	return PingResult{
		Version:         resp.GetExecutorVersion(),
		ProtocolVersion: resp.GetProtocolVersion(),
		EffectiveUID:    resp.GetEffectiveUid(),
	}, nil
}

// HostInfo, sunucunun donanım ve çekirdek bilgisini döner.
func (c *Client) HostInfo(ctx context.Context) (*panelyv1.HostInfo, error) {
	resp, err := c.rpc.GetHostInfo(ctx, &panelyv1.GetHostInfoRequest{})
	if err != nil {
		return nil, fmt.Errorf("host bilgisi alınamadı: %w", err)
	}
	return resp.GetHost(), nil
}

// JournalPage, executor denetim günlüğünün bir sayfasıdır.
type JournalPage struct {
	Records   []audit.Record
	LatestSeq uint64
}

// ReadJournal, executor'ın kendi denetim zincirini okur.
//
// Executor bu kayıtları teslim etmeden önce zinciri seq 1'den yeniden
// doğrular; buradan dönen kayıtlar okuma anında kanıtlanmıştır.
func (c *Client) ReadJournal(ctx context.Context, afterSeq uint64, limit uint32) (JournalPage, error) {
	resp, err := c.rpc.ReadAuditJournal(ctx, &panelyv1.ReadAuditJournalRequest{
		AfterSeq: afterSeq,
		Limit:    limit,
	})
	if err != nil {
		return JournalPage{}, fmt.Errorf("executor denetim günlüğü okunamadı: %w", err)
	}
	return JournalPage{
		Records:   auditRecordsFromProto(resp.GetRecords()),
		LatestSeq: resp.GetLatestSeq(),
	}, nil
}

// DefaultTimeout, executor çağrıları için makul bir üst sınırdır.
// Executor yerel bir unix soketindedir; yavaşlık ağ değil, sorun demektir.
const DefaultTimeout = 10 * time.Second

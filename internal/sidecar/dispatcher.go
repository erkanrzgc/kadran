// Package sidecar, masaüstünün sunucuyla konuştuğu yöntemlerin
// dağıtıcısıdır (status, audit.*, apps.*, alarms.list, backups.*,
// servers.*).
//
// İki taşıyıcı aynı dağıtıcıyı kullanır: `kadran sidecar` satır-bazlı
// JSON-RPC'yi stdio üzerinden sunar (Serve), masaüstü ise Dispatch'i süreç
// içinde çağırır (K-165). Yanıtların JSON biçimi ikisinde de aynıdır;
// arayüz hangisinin altında çalıştığını bilmez.
//
// Paket CLI'a bağlı değil: oturum, sunucu listesi, dal çözümü ve uyarılar
// Deps ile verilir.
package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
	"github.com/erkanrzgc/kadran/internal/version"
)

// JSON-RPC 2.0 standart hata kodları.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// Error, bir yöntemin hatası. Biçim (`message` sabit sınıf adı, gerçek
// sebep `data`'da) desktop/test/fixtures/sidecar-error.json ile sabit.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// Deps, dağıtıcının dışarıdan aldıkları. Boş bırakılan alan o özelliği
// kapatır: LoadSession yoksa oturum eklenmez, Warn yoksa uyarı yutulur.
type Deps struct {
	// CallTimeout, tek bir okuma çağrısının süresi; yönetim yöntemleri
	// kendi sürelerini kullanır (management.go).
	CallTimeout time.Duration
	// Servers, ortak sunucu listesi (servers.json, K-162).
	Servers servers.Store
	// LoadSession, hedefin kayıtlı TOTP oturum jetonu (K-155).
	LoadSession func(rawTarget string) (string, error)
	// ResolveCommit, apps.deploy'un dal ya da commit'ini tam sha'ya çevirir.
	ResolveCommit func(ctx context.Context, rpc kadranv1.KadranServiceClient, appID, commit, branch string) (string, error)
	// Warn, çağrıyı durdurmayan sorunlar (okunamayan oturum dosyası).
	Warn func(message string)
	// HideConsole, Windows'ta ssh'ye konsol penceresi açtırmaz (masaüstü;
	// bkz. client.DialOptions).
	HideConsole bool
}

// Dispatcher, yöntemleri çalıştırır ve hedef başına bağlantıları saklar.
// Eşzamanlı kullanım için güvenlidir.
type Dispatcher struct {
	deps Deps

	// conns, hedef başına açık bağlantıları saklar.
	//
	// Yeniden kullanmak SSH'ta ciddi fark yaratır: her çağrıda yeni bir
	// `ssh` alt süreci başlatmak, el sıkışma yüzünden yüzlerce
	// milisaniye ekler ve GUI hissedilir biçimde yavaşlar.
	mu    sync.Mutex
	conns map[string]*client.Client

	// serversMu, servers.* çağrılarını sıraya koyar (servers.go).
	serversMu sync.Mutex
}

// New, bir dağıtıcı kurar. İşi bitince Close çağrılmalı.
func New(deps Deps) *Dispatcher {
	return &Dispatcher{deps: deps, conns: make(map[string]*client.Client)}
}

// targetParams, hedef alan tüm metotların ortak parametreleridir.
type targetParams struct {
	Target string `json:"target"`
}

// Dispatch, bir yöntemi çalıştırır. Sonuç JSON'a çevrilebilir bir değerdir.
func (d *Dispatcher) Dispatch(ctx context.Context, method string, params json.RawMessage) (any, *Error) {
	switch method {
	case "apps.list", "apps.get", "apps.create", "apps.update", "apps.delete", "apps.deploy", "apps.rollback", "apps.logs", "alarms.list", "backups.list", "backups.create":
		return d.dispatchManagement(ctx, method, params)
	case "servers.list", "servers.add", "servers.update", "servers.remove", "servers.import":
		return d.dispatchServers(method, params)
	case "version":
		return map[string]any{
			"version":  version.Version,
			"commit":   version.Commit,
			"protocol": version.Protocol,
		}, nil

	case "status":
		return d.withConn(ctx, params, func(ctx context.Context, conn *client.Client) (any, error) {
			info, err := conn.RPC().GetSystemInfo(ctx, &kadranv1.GetSystemInfoRequest{})
			if err != nil {
				return nil, err
			}
			return ProtoToJSON(info)
		})

	case "audit.list":
		var p struct {
			targetParams
			AfterSeq uint64 `json:"after_seq"`
			Limit    uint32 `json:"limit"`
		}
		if err := decodeParams(params, &p); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		return d.withConn(ctx, params, func(ctx context.Context, conn *client.Client) (any, error) {
			resp, err := conn.RPC().ListAuditRecords(ctx, &kadranv1.ListAuditRecordsRequest{
				AfterSeq: p.AfterSeq,
				Limit:    p.Limit,
			})
			if err != nil {
				return nil, err
			}
			return ProtoToJSON(resp)
		})

	case "audit.verify":
		return d.withConn(ctx, params, func(ctx context.Context, conn *client.Client) (any, error) {
			resp, err := conn.RPC().VerifyAuditChain(ctx, &kadranv1.VerifyAuditChainRequest{})
			if err != nil {
				return nil, err
			}
			return ProtoToJSON(resp)
		})

	case "disconnect":
		var p targetParams
		if err := decodeParams(params, &p); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		d.closeTarget(p.Target)
		return map[string]any{"closed": true}, nil

	default:
		return nil, &Error{
			Code:    CodeMethodNotFound,
			Message: fmt.Sprintf("unknown method %q", method),
		}
	}
}

// withConn, hedefe bağlanır (veya açık bağlantıyı yeniden kullanır) ve
// verilen işi çalıştırır.
func (d *Dispatcher) withConn(
	ctx context.Context,
	params json.RawMessage,
	fn func(context.Context, *client.Client) (any, error),
) (any, *Error) {
	return d.withConnTimeout(ctx, params, d.deps.CallTimeout, fn)
}

func (d *Dispatcher) withConnTimeout(ctx context.Context, params json.RawMessage, timeout time.Duration, fn func(context.Context, *client.Client) (any, error)) (any, *Error) {
	var p targetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := d.connFor(ctx, p.Target)
	if err != nil {
		return nil, connectFailure(err)
	}
	d.refreshSession(conn, p.Target)

	if err := ctx.Err(); err != nil {
		return nil, &Error{Code: CodeInternalError, Message: "call timed out before execution", Data: err.Error()}
	}

	result, err := fn(ctx, conn)
	if err != nil {
		if shouldDropConnection(err) {
			d.closeTarget(p.Target)
		}
		return nil, callFailure(err)
	}
	return result, nil
}

// refreshSession, kayıtlı TOTP oturumunu her çağrıda yeniden okuyup
// bağlantıya koyar (K-155): masaüstü açıkken CLI'da yapılan `kadran login`
// ya da logout, önbellekteki bağlantının bir sonraki çağrısında geçerli.
// Bağlantı açıkken TOTP açılmış olabileceğinden Ping'e bakılmıyor; TOTP
// kapalı sunucu jetonu yok sayar. Okunamayan dosya çağrıyı durdurmaz.
func (d *Dispatcher) refreshSession(conn *client.Client, rawTarget string) {
	if d.deps.LoadSession == nil {
		return
	}
	tok, err := d.deps.LoadSession(rawTarget)
	if err != nil && d.deps.Warn != nil {
		d.deps.Warn(err.Error())
	}
	conn.SetSession(tok)
}

// connectFailure ve callFailure, masaüstüne giden hata biçiminin TEK
// kaynağıdır: `message` sabit bir sınıf adı, gerçek sebep `data`'dadır.
// Masaüstü ikisini birleştirip gösterir; biçim desktop/test/fixtures/
// sidecar-error.json altın dosyasıyla sabitlenir (error_test.go).
func connectFailure(err error) *Error {
	return &Error{Code: CodeInternalError, Message: "could not connect", Data: err.Error()}
}

func callFailure(err error) *Error {
	return &Error{Code: CodeInternalError, Message: "call failed", Data: err.Error()}
}

// shouldDropConnection, hatanın önbellekteki bağlantıyı düşürmeyi
// gerektirip gerektirmediğini söyler.
//
// # Neden her hatada düşürmüyoruz?
//
// Önbelleğin varlık nedeni SSH el sıkışmasından kaçınmak: her çağrıda yeni
// bir `ssh` alt süreci başlatmak yüzlerce milisaniye ekler ve GUI
// hissedilir biçimde yavaşlar.
//
// Uygulama düzeyindeki bir hata — anlık kilitli SQLite, geçersiz parametre,
// bulunamayan kayıt — bağlantı hakkında hiçbir şey söylemez. Böyle bir
// hatada bağlantıyı yıkmak, önbelleği tam da işe yaraması gereken anda
// (arka arkaya çağrılar) devre dışı bırakırdı.
//
// Yalnızca `Unavailable` taşımanın koptuğunu bildirir; bu durumda saklanan
// bağlantı zaten ölüdür ve düşürülmesi doğrudur.
func shouldDropConnection(err error) bool {
	return status.Code(err) == codes.Unavailable
}

// connFor, hedefe açık bir bağlantı döndürür; yoksa kurar.
//
// # Bilinen sınır
//
// Kilit, bağlantı kurulurken de tutuluyor. Yani YENİ bir hedefe bağlanmak
// (SSH'ta ConnectTimeout=10) o sırada başka bir hedefe bağlanmak isteyen
// istekleri bekletir. Sunucuya dokunmayan metotlar (`version`) etkilenmez.
//
// Bilerek böyle bırakıldı: çift kontrollü kilitleme kodu karmaşıklaştırır
// ve masaüstü kullanımında aynı anda birden fazla YENİ sunucuya bağlanmak
// ender bir durum. Masaüstü bunun sorun olduğunu gösterirse düzeltilir —
// şimdilik varsayım değil, ölçüm beklenmeli.
func (d *Dispatcher) connFor(ctx context.Context, rawTarget string) (*client.Client, error) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return nil, err
	}
	key := target.String()

	d.mu.Lock()
	defer d.mu.Unlock()

	if conn, ok := d.conns[key]; ok {
		return conn, nil
	}

	if !target.IsLocal() && !client.SSHAvailable() {
		return nil, errors.New("`ssh` not found")
	}

	conn, err := client.DialWith(target, client.DialOptions{HideConsole: d.deps.HideConsole})
	if err != nil {
		return nil, err
	}

	// Protokol uyumu ilk bağlantıda doğrulanır: uyumsuz bir sunucuyla
	// GUI'nin yarı çalışır görünmesindense hemen hata vermek iyidir.
	checkCtx, cancel := context.WithTimeout(ctx, d.deps.CallTimeout)
	defer cancel()
	if _, err := conn.CheckProtocol(checkCtx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	d.conns[key] = conn
	return conn, nil
}

func (d *Dispatcher) closeTarget(rawTarget string) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return
	}
	key := target.String()

	d.mu.Lock()
	defer d.mu.Unlock()

	if conn, ok := d.conns[key]; ok {
		_ = conn.Close()
		delete(d.conns, key)
	}
}

// Close, açık bütün bağlantıları kapatır. Arkada SSH süreçleri kalmasın
// diye taşıyıcı kapanırken çağrılır.
func (d *Dispatcher) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for key, conn := range d.conns {
		_ = conn.Close()
		delete(d.conns, key)
	}
}

// decodeParams, parametreleri çözümler. Boş params geçerlidir.
func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("could not parse the parameters: %w", err)
	}
	return nil
}

// ProtoToJSON, bir protobuf mesajını JSON'a çevirir.
//
// EmitUnpopulated açık: boş alanlar da yazılır. Tüketici (masaüstü, jq,
// betikler) böylece "alan yok" ile "alan boş" ayrımını yapmak zorunda
// kalmaz; şema neyse çıktı odur.
func ProtoToJSON(m proto.Message) (json.RawMessage, error) {
	opts := protojson.MarshalOptions{
		EmitUnpopulated: true,
		UseProtoNames:   true, // .proto'daki alan adları — şemayla birebir
	}
	b, err := opts.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("could not encode as JSON: %w", err)
	}
	return b, nil
}

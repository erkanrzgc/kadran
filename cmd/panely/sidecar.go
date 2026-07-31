package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/client"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/version"
)

// # Sidecar neden stdio, neden yerel port değil?
//
// Electron kabuğu `panely sidecar`'ı çocuk süreç olarak başlatır ve
// stdin/stdout üzerinden satır-bazlı JSON-RPC 2.0 konuşur (LSP ve MCP'nin
// deseni). Yerel bir TCP portu açmak üç sorun getirirdi: makinedeki her
// süreç ona bağlanabilirdi, bir kimlik doğrulama katmanı icat etmek
// gerekirdi ve güvenlik duvarı istemleri kullanıcıyı rahatsız ederdi.
// Boru, işletim sisteminin süreç ilişkisiyle zaten kapalı bir kanaldır.
//
// Satır-bazlı çerçeveleme: her istek tek satır JSON, her yanıt tek satır
// JSON. Content-Length başlığı (LSP'deki gibi) burada gereksiz karmaşa.

// jsonrpcVersion, protokolün zorunlu sürüm alanıdır.
const jsonrpcVersion = "2.0"

// maxRequestBytes, tek bir istek satırının üst sınırı.
//
// Bozuk veya kötü niyetli bir üst süreç sonsuz uzunlukta bir satır
// gönderip belleği tüketmesin diye.
const maxRequestBytes = 1 << 20 // 1 MiB

// JSON-RPC 2.0 standart hata kodları.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

// targetParams, hedef alan tüm metotların ortak parametreleridir.
type targetParams struct {
	Target string `json:"target"`
}

// runSidecar, stdio üzerinde JSON-RPC sunucusu çalıştırır.
func (c *cli) runSidecar(ctx context.Context, args []string) int {
	fs := c.newFlagSet("sidecar")
	timeout := fs.Duration("timeout", defaultTimeout, "tek bir çağrı için süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		return c.usageError("`sidecar` argüman almaz")
	}

	s := &sidecar{
		cli:         c,
		callTimeout: *timeout,
		conns:       make(map[string]*client.Client),
	}
	defer s.closeAll()

	if err := s.serve(ctx); err != nil {
		return c.fail(err)
	}
	return exitOK
}

type sidecar struct {
	cli         *cli
	callTimeout time.Duration

	// conns, hedef başına açık bağlantıları saklar.
	//
	// Yeniden kullanmak SSH'ta ciddi fark yaratır: her çağrıda yeni bir
	// `ssh` alt süreci başlatmak, el sıkışma yüzünden yüzlerce
	// milisaniye ekler ve GUI hissedilir biçimde yavaşlar.
	mu    sync.Mutex
	conns map[string]*client.Client

	// writeMu, yanıtların stdout'ta birbirine karışmasını engeller.
	//
	// İstekler eşzamanlı işlendiği için iki yanıt aynı anda yazmaya
	// çalışabilir. Çerçeveleme satır sonuna dayandığından araya giren
	// bir yazma, üst sürecin akışı yanlış bölmesine yol açardı.
	writeMu sync.Mutex
}

// serve, stdin'den gelen istekleri okur ve her birini eşzamanlı işler.
//
// # Neden eşzamanlı?
//
// Seri işleseydik, yavaş bir uzak `status` çağrısı — süre sınırına kadar
// 30 saniye — arkasındaki HER isteği bloklardı, `version` gibi sunucuya
// hiç dokunmayanları bile. GUI'nin tamamı donardı.
//
// JSON-RPC yanıtları `id` ile eşleştiği için sıra dışı dönmeleri sorun
// değil; protokol zaten bunu öngörüyor.
func (s *sidecar) serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.cli.stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRequestBytes)

	var wg sync.WaitGroup
	// Uçuştaki işler bitmeden dönmeyiz: aksi hâlde çağıranın closeAll'ı
	// hâlâ kullanılan bağlantıları kapatırdı.
	defer wg.Wait()

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		if len(scanner.Bytes()) == 0 {
			continue
		}

		// Scanner tamponunu bir sonraki Scan'de YENİDEN KULLANIR. Diliminin
		// kendisini goroutine'e vermek, üzerine yazılan bir tamponu okumak
		// demek olurdu — klasik ve sessiz bir veri yarışı.
		line := append([]byte(nil), scanner.Bytes()...)

		wg.Add(1)
		go func() {
			defer wg.Done()
			s.handleLine(ctx, line)
		}()
	}

	// stdin kapandığında Scanner.Err() nil döner; EOF hata sayılmaz.
	// Buraya bir hata geldiyse gerçekten okuma bozulmuş demektir.
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("sidecar girdisi okunamadı: %w", err)
	}
	return nil
}

func (s *sidecar) handleLine(ctx context.Context, line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(rpcResponse{
			JSONRPC: jsonrpcVersion,
			Error:   &rpcError{Code: codeParseError, Message: "JSON çözümlenemedi", Data: err.Error()},
		})
		return
	}

	if req.JSONRPC != jsonrpcVersion || req.Method == "" {
		s.reply(rpcResponse{
			JSONRPC: jsonrpcVersion,
			ID:      req.ID,
			Error: &rpcError{
				Code:    codeInvalidRequest,
				Message: `geçersiz istek: "jsonrpc":"2.0" ve "method" zorunlu`,
			},
		})
		return
	}

	// ID yoksa bu bir bildirimdir (notification) ve yanıtlanmaz.
	// Yine de işi yaparız; JSON-RPC 2.0 böyle tanımlar.
	result, rpcErr := s.dispatch(ctx, req)
	if len(req.ID) == 0 {
		return
	}

	resp := rpcResponse{JSONRPC: jsonrpcVersion, ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	s.reply(resp)
}

func (s *sidecar) dispatch(ctx context.Context, req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "version":
		return map[string]any{
			"version":  version.Version,
			"commit":   version.Commit,
			"protocol": version.Protocol,
		}, nil

	case "status":
		return s.withConn(ctx, req.Params, func(ctx context.Context, conn *client.Client) (any, error) {
			info, err := conn.RPC().GetSystemInfo(ctx, &panelyv1.GetSystemInfoRequest{})
			if err != nil {
				return nil, err
			}
			return protoToJSON(info)
		})

	case "audit.list":
		var p struct {
			targetParams
			AfterSeq uint64 `json:"after_seq"`
			Limit    uint32 `json:"limit"`
		}
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		return s.withConn(ctx, req.Params, func(ctx context.Context, conn *client.Client) (any, error) {
			resp, err := conn.RPC().ListAuditRecords(ctx, &panelyv1.ListAuditRecordsRequest{
				AfterSeq: p.AfterSeq,
				Limit:    p.Limit,
			})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		})

	case "audit.verify":
		return s.withConn(ctx, req.Params, func(ctx context.Context, conn *client.Client) (any, error) {
			resp, err := conn.RPC().VerifyAuditChain(ctx, &panelyv1.VerifyAuditChainRequest{})
			if err != nil {
				return nil, err
			}
			return protoToJSON(resp)
		})

	case "disconnect":
		var p targetParams
		if err := decodeParams(req.Params, &p); err != nil {
			return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
		}
		s.closeTarget(p.Target)
		return map[string]any{"closed": true}, nil

	default:
		return nil, &rpcError{
			Code:    codeMethodNotFound,
			Message: fmt.Sprintf("bilinmeyen metot %q", req.Method),
		}
	}
}

// withConn, hedefe bağlanır (veya açık bağlantıyı yeniden kullanır) ve
// verilen işi çalıştırır.
func (s *sidecar) withConn(
	ctx context.Context,
	params json.RawMessage,
	fn func(context.Context, *client.Client) (any, error),
) (any, *rpcError) {
	var p targetParams
	if err := decodeParams(params, &p); err != nil {
		return nil, &rpcError{Code: codeInvalidParams, Message: err.Error()}
	}

	conn, err := s.connFor(ctx, p.Target)
	if err != nil {
		return nil, &rpcError{Code: codeInternalError, Message: "bağlanılamadı", Data: err.Error()}
	}

	ctx, cancel := context.WithTimeout(ctx, s.callTimeout)
	defer cancel()

	result, err := fn(ctx, conn)
	if err != nil {
		if shouldDropConnection(err) {
			s.closeTarget(p.Target)
		}
		return nil, &rpcError{Code: codeInternalError, Message: "çağrı başarısız", Data: err.Error()}
	}
	return result, nil
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
// ender bir durum. Elektron kabuğu bunun sorun olduğunu gösterirse
// düzeltilir — şimdilik varsayım değil, ölçüm beklenmeli.
func (s *sidecar) connFor(ctx context.Context, rawTarget string) (*client.Client, error) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return nil, err
	}
	key := target.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if conn, ok := s.conns[key]; ok {
		return conn, nil
	}

	if !target.IsLocal() && !client.SSHAvailable() {
		return nil, errors.New("`ssh` komutu bulunamadı")
	}

	conn, err := client.Dial(target)
	if err != nil {
		return nil, err
	}

	// Protokol uyumu ilk bağlantıda doğrulanır: uyumsuz bir sunucuyla
	// GUI'nin yarı çalışır görünmesindense hemen hata vermek iyidir.
	checkCtx, cancel := context.WithTimeout(ctx, s.callTimeout)
	defer cancel()
	if _, err := conn.CheckProtocol(checkCtx); err != nil {
		_ = conn.Close()
		return nil, err
	}

	s.conns[key] = conn
	return conn, nil
}

func (s *sidecar) closeTarget(rawTarget string) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return
	}
	key := target.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	if conn, ok := s.conns[key]; ok {
		_ = conn.Close()
		delete(s.conns, key)
	}
}

func (s *sidecar) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, conn := range s.conns {
		_ = conn.Close()
		delete(s.conns, key)
	}
}

// reply, tek satırlık bir yanıt yazar.
//
// Çerçeveleme satır sonuna dayandığı için yanıtın içinde ham satır sonu
// olamaz; json.Encoder zaten kaçışlar.
func (s *sidecar) reply(resp rpcResponse) {
	body, err := json.Marshal(resp)
	if err != nil {
		// Yanıt kodlanamıyorsa en azından hatayı bildirebilmeliyiz.
		body, _ = json.Marshal(rpcResponse{
			JSONRPC: jsonrpcVersion,
			ID:      resp.ID,
			Error:   &rpcError{Code: codeInternalError, Message: "yanıt kodlanamadı"},
		})
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	fmt.Fprintf(s.cli.stdout, "%s\n", body)
}

// decodeParams, parametreleri çözümler. Boş params geçerlidir.
func decodeParams(raw json.RawMessage, dst any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("parametreler çözümlenemedi: %w", err)
	}
	return nil
}

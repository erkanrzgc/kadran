package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

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
}

func (s *sidecar) serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.cli.stdin)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRequestBytes)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		s.handleLine(ctx, line)
	}

	if err := scanner.Err(); err != nil {
		// stdin kapandıysa üst süreç gitmiştir; bu normal bir son.
		if errors.Is(err, io.EOF) {
			return nil
		}
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
		// Bağlantı bozulmuş olabilir; önbellekten düşür ki bir sonraki
		// çağrı temiz bir bağlantıyla denesin.
		s.closeTarget(p.Target)
		return nil, &rpcError{Code: codeInternalError, Message: "çağrı başarısız", Data: err.Error()}
	}
	return result, nil
}

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

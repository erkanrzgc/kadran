package sidecar

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// # Neden stdio, neden yerel port değil?
//
// `kadran sidecar` stdin/stdout üzerinden satır-bazlı JSON-RPC 2.0 konuşur
// (LSP ve MCP'nin deseni). Yerel bir TCP portu açmak üç sorun getirirdi:
// makinedeki her süreç ona bağlanabilirdi, bir kimlik doğrulama katmanı
// icat etmek gerekirdi ve güvenlik duvarı istemleri kullanıcıyı rahatsız
// ederdi. Boru, işletim sisteminin süreç ilişkisiyle zaten kapalı bir
// kanaldır.
//
// Satır-bazlı çerçeveleme: her istek tek satır JSON, her yanıt tek satır
// JSON. Content-Length başlığı (LSP'deki gibi) burada gereksiz karmaşa.

// JSONRPCVersion, protokolün zorunlu sürüm alanıdır.
const JSONRPCVersion = "2.0"

// maxRequestBytes, tek bir istek satırının üst sınırı.
//
// Bozuk veya kötü niyetli bir üst süreç sonsuz uzunlukta bir satır
// gönderip belleği tüketmesin diye.
const maxRequestBytes = 1 << 20 // 1 MiB

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response, tek bir JSON-RPC yanıtı (tek satır).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// stdio, tek bir Serve çağrısının durumu.
type stdio struct {
	d   *Dispatcher
	out io.Writer

	// writeMu, yanıtların çıktıda birbirine karışmasını engeller.
	//
	// İstekler eşzamanlı işlendiği için iki yanıt aynı anda yazmaya
	// çalışabilir. Çerçeveleme satır sonuna dayandığından araya giren
	// bir yazma, üst sürecin akışı yanlış bölmesine yol açardı.
	writeMu sync.Mutex
}

// Serve, in'den gelen istekleri okur ve her birini eşzamanlı işler;
// yanıtları out'a yazar. in kapanınca, uçuştaki işler bitince döner.
//
// # Neden eşzamanlı?
//
// Seri işleseydik, yavaş bir uzak `status` çağrısı — süre sınırına kadar
// 30 saniye — arkasındaki HER isteği bloklardı, `version` gibi sunucuya
// hiç dokunmayanları bile. GUI'nin tamamı donardı.
//
// JSON-RPC yanıtları `id` ile eşleştiği için sıra dışı dönmeleri sorun
// değil; protokol zaten bunu öngörüyor.
func Serve(ctx context.Context, d *Dispatcher, in io.Reader, out io.Writer) error {
	s := &stdio{d: d, out: out}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), maxRequestBytes)

	var wg sync.WaitGroup
	// Uçuştaki işler bitmeden dönmeyiz: aksi hâlde çağıranın Close'u
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

	// in kapandığında Scanner.Err() nil döner; EOF hata sayılmaz.
	// Buraya bir hata geldiyse gerçekten okuma bozulmuş demektir.
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("could not read sidecar input: %w", err)
	}
	return nil
}

func (s *stdio) handleLine(ctx context.Context, line []byte) {
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		s.reply(Response{
			JSONRPC: JSONRPCVersion,
			Error:   &Error{Code: CodeParseError, Message: "could not parse JSON", Data: err.Error()},
		})
		return
	}

	if req.JSONRPC != JSONRPCVersion || req.Method == "" {
		s.reply(Response{
			JSONRPC: JSONRPCVersion,
			ID:      req.ID,
			Error: &Error{
				Code:    CodeInvalidRequest,
				Message: `invalid request: "jsonrpc":"2.0" and "method" are required`,
			},
		})
		return
	}

	// ID yoksa bu bir bildirimdir (notification) ve yanıtlanmaz.
	// Yine de işi yaparız; JSON-RPC 2.0 böyle tanımlar.
	result, rpcErr := s.d.Dispatch(ctx, req.Method, req.Params)
	if len(req.ID) == 0 {
		return
	}

	resp := Response{JSONRPC: JSONRPCVersion, ID: req.ID}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}
	s.reply(resp)
}

// reply, tek satırlık bir yanıt yazar.
//
// Çerçeveleme satır sonuna dayandığı için yanıtın içinde ham satır sonu
// olamaz; json.Encoder zaten kaçışlar.
func (s *stdio) reply(resp Response) {
	body, err := json.Marshal(resp)
	if err != nil {
		// Yanıt kodlanamıyorsa en azından hatayı bildirebilmeliyiz.
		body, _ = json.Marshal(Response{
			JSONRPC: JSONRPCVersion,
			ID:      resp.ID,
			Error:   &Error{Code: CodeInternalError, Message: "could not encode the response"},
		})
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	fmt.Fprintf(s.out, "%s\n", body)
}

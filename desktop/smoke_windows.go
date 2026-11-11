//go:build windows && smoke

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"
)

// Duman testinin sayfaya ulaşma yolu (yalnız `-tags smoke`).
//
// go-webview2, WebView2'ye ortam ya da kayıt defteri üzerinden tarayıcı
// argümanı verilmesini kasıtlı olarak engelliyor; hata ayıklama portu
// (CDP) açılamıyor. Bunun yerine koşucu (scripts/smoke.ts) stdin'den tek
// satırlık komut yollar: {"id", "code"} sayfada çalıştırılır (ExecJS),
// sonuç Wails olayıyla döner ve stdout'a "SMOKE " önekiyle yazılır;
// {"id", "capture"} pencerenin görüntüsünü PNG olarak kaydeder.

const protocolPrefix = "SMOKE "

// collector, sayfa betiklerinden ÖNCE yüklenir: konsol hataları, yakalanmayan
// hatalar ve CSP ihlalleri window.__smokeProblems'a düşer.
const collector = `(() => {
  const problems = [];
  window.__smokeProblems = problems;
  window.addEventListener("error", (e) => problems.push("uncaught: " + (e.message || (e.target && e.target.src) || e.type)), true);
  window.addEventListener("unhandledrejection", (e) => problems.push("unhandled rejection: " + String(e.reason)));
  document.addEventListener("securitypolicyviolation", (e) => problems.push("CSP: " + e.violatedDirective + " " + e.blockedURI));
  const original = console.error.bind(console);
  console.error = (...args) => { problems.push("console error: " + args.map(String).join(" ")); original(...args); };
})();
`

// smokeMiddleware, toplayıcıyı sunar ve sayfanın <head>'inin başına ekler.
func smokeMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/smoke/collector.js" {
			w.Header().Set("Content-Type", "text/javascript")
			_, _ = w.Write([]byte(collector))
			return
		}
		if r.URL.Path != "/" && r.URL.Path != "/index.html" {
			next.ServeHTTP(w, r)
			return
		}
		rec := httptest.NewRecorder()
		next.ServeHTTP(rec, r)
		body := bytes.Replace(rec.Body.Bytes(), []byte("<head>"), []byte(`<head><script src="/smoke/collector.js"></script>`), 1)
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.Header().Del("Content-Length")
		w.WriteHeader(rec.Code)
		_, _ = w.Write(body)
	})
}

type smokeCommand struct {
	ID      int64  `json:"id"`
	Code    string `json:"code"`
	Capture string `json:"capture"`
}

var smokeOnce sync.Once

// startSmoke, sayfa hazır olunca komutları dinlemeye başlar.
func startSmoke(ctx context.Context, h *host) {
	smokeOnce.Do(func() {
		var outMu sync.Mutex
		reply := func(v any) {
			line, err := json.Marshal(v)
			if err != nil {
				line = []byte(`{"error":"could not encode the reply"}`)
			}
			outMu.Lock()
			fmt.Fprintf(os.Stdout, "%s%s\n", protocolPrefix, line)
			outMu.Unlock()
		}
		runtime.EventsOn(ctx, "smoke:eval", func(data ...any) {
			if len(data) == 1 {
				reply(data[0])
			}
		})
		go func() {
			scanner := bufio.NewScanner(os.Stdin)
			scanner.Buffer(make([]byte, 0, 64*1024), 8<<20)
			for scanner.Scan() {
				var cmd smokeCommand
				if err := json.Unmarshal(scanner.Bytes(), &cmd); err != nil {
					reply(map[string]any{"id": 0, "ok": false, "error": "bad command: " + err.Error()})
					continue
				}
				if cmd.Capture != "" {
					err := capture(h.window(), cmd.Capture)
					result := map[string]any{"id": cmd.ID, "ok": err == nil}
					if err != nil {
						result["error"] = err.Error()
					}
					reply(result)
					continue
				}
				runtime.WindowExecJS(ctx, wrap(cmd.ID, cmd.Code))
			}
		}()
	})
}

// wrap: kod bir ifade; sonucu (ya da hatası) olayla döner.
func wrap(id int64, code string) string {
	n := strconv.FormatInt(id, 10)
	return `(async () => { let r; try { r = { id: ` + n + `, ok: true, value: await (` + code + `) }; } catch (e) { r = { id: ` + n + `, ok: false, error: String((e && e.stack) || e) }; } ` +
		`try { JSON.stringify(r); } catch (e) { r = { id: ` + n + `, ok: false, error: "result is not serializable: " + String(e) }; } window.runtime.EventsEmit("smoke:eval", r); })();`
}

var (
	gdi32                  = windows.NewLazySystemDLL("gdi32.dll")
	user32smoke            = windows.NewLazySystemDLL("user32.dll")
	procGetDC              = user32smoke.NewProc("GetDC")
	procReleaseDC          = user32smoke.NewProc("ReleaseDC")
	procGetClientRect      = user32smoke.NewProc("GetClientRect")
	procPrintWindow        = user32smoke.NewProc("PrintWindow")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

type bitmapInfoHeader struct {
	Size          uint32
	Width, Height int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// capture, pencerenin içeriğini PNG olarak kaydeder (PrintWindow,
// PW_RENDERFULLCONTENT: WebView2'nin bileşik çizimi de gelir).
func capture(hwnd windows.HWND, path string) error {
	if hwnd == 0 {
		return fmt.Errorf("no window")
	}
	var rect struct{ Left, Top, Right, Bottom int32 }
	if r, _, err := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect))); r == 0 {
		return fmt.Errorf("GetClientRect: %w", err)
	}
	w, h := int(rect.Right-rect.Left), int(rect.Bottom-rect.Top)
	if w <= 0 || h <= 0 {
		return fmt.Errorf("empty window %dx%d", w, h)
	}
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen) //nolint:errcheck
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	defer procDeleteDC.Call(mem) //nolint:errcheck
	info := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	bmp, _, err := procCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return fmt.Errorf("CreateDIBSection: %w", err)
	}
	defer procDeleteObject.Call(bmp) //nolint:errcheck
	procSelectObject.Call(mem, bmp)  //nolint:errcheck
	const pwRenderFullContent = 2
	if r, _, err := procPrintWindow.Call(uintptr(hwnd), mem, pwRenderFullContent); r == 0 {
		return fmt.Errorf("PrintWindow: %w", err)
	}
	src := unsafe.Slice((*byte)(bits), w*h*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = src[i*4+2], src[i*4+1], src[i*4], 255
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o600)
}

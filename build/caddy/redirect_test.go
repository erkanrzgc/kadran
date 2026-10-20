package main

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// redirectLocation, kadran-proxy'nin yönlendirme rotasında kullandığı TEK
// Location şablonu (K-152). Hedef ad doğrulanmış bir alan adı; değişken
// kısım yalnız {http.request.uri}.
const redirectLocation = "https://example.com{http.request.uri}"

func serveRedirect(t *testing.T, location, target string) *httptest.ResponseRecorder {
	t.Helper()
	// İstek, net/http'nin gerçek ayrıştırıcısıyla kuruluyor: Caddy'nin
	// gördüğü URL.Path/RawPath/RawQuery ile aynı.
	req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(
		"GET " + target + " HTTP/1.1\r\nHost: www.example.com\r\n\r\n")))
	if err != nil {
		t.Fatalf("%q: %v", target, err)
	}
	repl := caddyhttp.NewTestReplacer(req)
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, repl))
	w := httptest.NewRecorder()
	h := caddyhttp.StaticResponse{
		StatusCode: caddyhttp.WeakString("308"),
		Headers:    http.Header{"Location": []string{location}},
	}
	if err := h.ServeHTTP(w, req, nil); err != nil {
		t.Fatal(err)
	}
	return w
}

// Bir istek yolu {file.…} yer tutucusu taşısa da Location'a dosya içeriği
// girmemeli: değiştirici değerleri yeniden açarsa kadrand'ın kurduğu her
// yönlendirme sunucudaki dosyaları okuyan bir kapı olurdu.
func TestRedirectLocationDoesNotExpandRequestPlaceholders(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "synthetic-key"))
	const secret = "synthetic-caddy-key-do-not-use"
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	placeholder := "{file." + path + "}"

	// Kontrol grubu: şablonun kendisinde yer tutucu olsaydı sızardı. Bu,
	// testin bir sızıntıyı GÖREBİLDİĞİNİ kanıtlıyor.
	if got := serveRedirect(t, "https://example.com/"+placeholder, "/").Header().Get("Location"); !strings.Contains(got, secret) {
		t.Fatalf("kontrol grubu sızmadı (test sızıntıyı göremiyor): %q", got)
	}

	for _, target := range []string{
		"/" + placeholder,
		"/a?" + placeholder,
		"/%7Bfile." + path + "%7D",
		"/a?q=%7Bfile." + path + "%7D",
		"/{http.request.uri}",
		"/{env.PATH}",
	} {
		w := serveRedirect(t, redirectLocation, target)
		loc := w.Header().Get("Location")
		if w.Code != http.StatusPermanentRedirect {
			t.Errorf("%q: durum %d", target, w.Code)
		}
		if strings.Contains(loc, secret) || strings.Contains(loc, os.Getenv("PATH")) {
			t.Errorf("%q: Location yer tutucuyu açtı: %q", target, loc)
		}
		if !strings.HasPrefix(loc, "https://example.com/") {
			t.Errorf("%q: Location başka yere gidiyor: %q", target, loc)
		}
		t.Logf("%q -> %q", target, loc)
	}
}

// Yol ve sorgu korunur; CR/LF başlığa giremez.
func TestRedirectLocationKeepsPathAndQuery(t *testing.T) {
	for target, want := range map[string]string{
		"/":                      "https://example.com/",
		"/blog/post?id=7&x":      "https://example.com/blog/post?id=7&x",
		"/a%0d%0aSet-Cookie:x=1": "https://example.com/a%0d%0aSet-Cookie:x=1",
		"//evil.example/x":       "https://example.com//evil.example/x",
	} {
		got := serveRedirect(t, redirectLocation, target).Header().Get("Location")
		if got != want {
			t.Errorf("%q: Location %q, beklenen %q", target, got, want)
		}
		if strings.ContainsAny(got, "\r\n") {
			t.Errorf("%q: Location satır sonu taşıyor: %q", target, got)
		}
	}
}

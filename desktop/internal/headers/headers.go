// Package headers, arayüz dosyalarına eklenen güvenlik başlıkları.
//
// Wails, WebView2'de bütün izin isteklerini kendiliğinden onaylıyor
// (SetGlobalPermission(Allow)); Electron'da izinler kapalıydı, yalnız yazı
// tipi listesi açıktı (K-165). Belgeye verilen Permissions-Policy bu
// özellikleri tarayıcı düzeyinde kapatır: ele geçmiş bir arayüz kamerayı,
// mikrofonu ya da konumu isteyemez bile. Yazı tipi listesi (Görünüm
// ayarları) yalnız uygulamanın kökenine açık.
package headers

import (
	"net/http"
	"strings"
)

// denied, kapatılan tarayıcı özellikleri.
var denied = []string{
	"camera", "microphone", "geolocation", "display-capture", "usb", "serial", "hid", "bluetooth",
	"midi", "payment", "clipboard-read", "clipboard-write", "idle-detection", "screen-wake-lock",
	"window-management", "fullscreen", "publickey-credentials-get", "storage-access",
}

// PermissionsPolicy, belgeye verilen politika.
func PermissionsPolicy() string {
	parts := make([]string, 0, len(denied)+1)
	for _, f := range denied {
		parts = append(parts, f+"=()")
	}
	parts = append(parts, "local-fonts=(self)")
	return strings.Join(parts, ", ")
}

// Middleware, Wails'in varlık sunucusuna başlıkları ekler.
func Middleware(next http.Handler) http.Handler {
	policy := PermissionsPolicy()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Permissions-Policy", policy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

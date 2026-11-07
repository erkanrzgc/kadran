package headers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyDeniesPowerfulFeaturesAllowsOnlyFonts(t *testing.T) {
	p := PermissionsPolicy()
	for _, f := range []string{"camera=()", "microphone=()", "geolocation=()", "clipboard-read=()", "display-capture=()"} {
		if !strings.Contains(p, f) {
			t.Errorf("%s yok: %s", f, p)
		}
	}
	if !strings.HasSuffix(p, "local-fonts=(self)") || strings.Count(p, "(self)") != 1 || strings.Contains(p, "*") {
		t.Errorf("yalnız yazı tipi listesi açık olmalı: %s", p)
	}
}

func TestMiddlewareAddsHeadersAndCallsNext(t *testing.T) {
	called := false
	h := Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/index.html", nil))
	if !called || rec.Header().Get("Permissions-Policy") != PermissionsPolicy() || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("başlıklar %v", rec.Header())
	}
}

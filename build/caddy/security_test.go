package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

// This reproduces why module exclusions alone are not a security boundary.
// The synthetic file is read as Caddy's own identity even without fileserver
// or templates. Only the separate typed broker excludes this configuration.
func TestCoreStaticResponseCanReadFilePlaceholder(t *testing.T) {
	for _, name := range []string{"http.handlers.file_server", "http.handlers.templates"} {
		if _, err := caddy.GetModule(name); err == nil {
			t.Fatalf("unexpected module %s", name)
		}
	}
	if _, err := caddy.GetModule("http.handlers.reverse_proxy"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "synthetic-key")
	const secret = "synthetic-caddy-key-do-not-use"
	if err := os.WriteFile(path, []byte(secret), 0600); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "https://example.invalid", nil)
	req = req.WithContext(context.WithValue(req.Context(), caddy.ReplacerCtxKey, caddy.NewReplacer()))
	w := httptest.NewRecorder()
	handler := caddyhttp.StaticResponse{Body: "{file." + path + "}"}
	if err := handler.ServeHTTP(w, req, nil); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != secret {
		t.Fatalf("file placeholder capability changed: %q", w.Body.String())
	}
}

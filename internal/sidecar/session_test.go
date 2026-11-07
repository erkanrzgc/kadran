package sidecar

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/client"
)

// TestSessionIsReloadedOnEveryCall: masaüstü açıkken CLI'da yapılan login ve
// logout, ÖNBELLEKTEKİ bağlantının bir sonraki çağrısında geçerli (K-155).
// Okunamayan oturum çağrıyı durdurmaz, uyarılır ve jeton boşalır.
func TestSessionIsReloadedOnEveryCall(t *testing.T) {
	saved, loadErr := "", error(nil)
	var warnings []string
	d := New(Deps{
		CallTimeout: 5 * time.Second,
		LoadSession: func(raw string) (string, error) {
			if raw != "srv.example" {
				t.Errorf("okuyucuya giden hedef %q", raw)
			}
			return saved, loadErr
		},
		Warn: func(message string) { warnings = append(warnings, message) },
	})
	defer d.Close()
	d.conns["kadran-client@srv.example"] = client.NewWithRPC(nil)

	params, err := json.Marshal(targetParams{Target: "srv.example"})
	if err != nil {
		t.Fatal(err)
	}
	seen := func() string {
		var tok string
		if _, fail := d.withConn(context.Background(), params, func(_ context.Context, c *client.Client) (any, error) {
			tok = c.Session()
			return nil, nil
		}); fail != nil {
			t.Fatalf("çağrı: %+v", fail)
		}
		return tok
	}

	if got := seen(); got != "" {
		t.Fatalf("oturum yokken jeton: %q", got)
	}
	saved = "v1.yuk.imza"
	if got := seen(); got != "v1.yuk.imza" {
		t.Fatalf("login'den sonra jeton %q", got)
	}
	saved, loadErr = "", errors.New("session file is damaged")
	if got := seen(); got != "" {
		t.Fatalf("okunamayan oturumdan sonra jeton %q", got)
	}
	if len(warnings) != 1 || warnings[0] != "session file is damaged" {
		t.Fatalf("uyarılar: %q", warnings)
	}
}

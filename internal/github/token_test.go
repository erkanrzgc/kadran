package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var tokenTestNow = time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)

const refreshOK = `{"access_token":"ghu_new","expires_in":28800,"refresh_token":"ghr_new","refresh_token_expires_in":15897600,"token_type":"bearer"}`

// newTestTokens, durum dosyasını yazıp verilen sahte yenileme uç noktasına
// bağlı bir tokens kurar.
func newTestTokens(t *testing.T, st state, h http.HandlerFunc) (*tokens, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, st); err != nil {
		t.Fatal(err)
	}
	return &tokens{
		path: path, clientID: testClientID, c: serverClient(t, h),
		now: func() time.Time { return tokenTestNow }, st: st, ok: true,
	}, path
}

// expiringState 5 dakikanın içinde bitecek erişim jetonlu durumdur.
func expiringState() state {
	st := sampleState()
	st.AccessExpiresAt = tokenTestNow.Add(time.Minute)
	st.RefreshExpiresAt = tokenTestNow.Add(24 * time.Hour)
	return st
}

func jsonHandler(body string, hits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func TestRefreshPersistsBeforeUse(t *testing.T) {
	var hits atomic.Int32
	tk, path := newTestTokens(t, expiringState(), jsonHandler(refreshOK, &hits))
	old := saveStateFn
	saveStateFn = func(string, state) error { return errors.New("disk full") }
	t.Cleanup(func() { saveStateFn = old })

	got, err := tk.Token(context.Background())
	if !errors.Is(err, ErrReconnect) || got != "" {
		t.Fatalf("Token = %q, %v; want empty + ErrReconnect", got, err)
	}
	if tk.ok || tk.st.AccessToken != "" || tk.st.RefreshToken != "" {
		t.Fatal("state must be dropped after a failed save")
	}
	if _, err := tk.Token(context.Background()); err == nil {
		t.Fatal("token served after failed save")
	}
	if hits.Load() != 1 {
		t.Fatalf("refresh requests = %d, want 1", hits.Load())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) || !tk.ReconnectRequired() {
		t.Fatalf("dead state file must be removed (stat err %v) and reconnect required", err)
	}
}

func TestRefreshIsSingleFlight(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, expiringState(), func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(refreshOK))
	})
	var wg sync.WaitGroup
	results := make([]string, 20)
	errs := make([]error, 20)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = tk.Token(context.Background())
		}()
	}
	wg.Wait()
	if hits.Load() != 1 {
		t.Fatalf("refresh requests = %d, want 1", hits.Load())
	}
	for i := range results {
		if errs[i] != nil || results[i] != "ghu_new" {
			t.Fatalf("caller %d got %q, %v", i, results[i], errs[i])
		}
	}
}

func TestRefreshRotatesBothTokens(t *testing.T) {
	var hits atomic.Int32
	tk, path := newTestTokens(t, expiringState(), jsonHandler(refreshOK, &hits))
	if _, err := tk.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var saved state
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "ghr_new" || saved.AccessToken != "ghu_new" || strings.Contains(string(raw), "ghr_old") || strings.Contains(string(raw), "ghu_old") {
		t.Fatalf("file not rotated: %s", raw)
	}
	if want := tokenTestNow.Add(28800 * time.Second); !saved.AccessExpiresAt.Equal(want) {
		t.Fatalf("access expiry = %v, want %v", saved.AccessExpiresAt, want)
	}
	if want := tokenTestNow.Add(15897600 * time.Second); !saved.RefreshExpiresAt.Equal(want) {
		t.Fatalf("refresh expiry = %v, want %v", saved.RefreshExpiresAt, want)
	}
	if saved.Login != "octo" || saved.UserID != 42 || !saved.ConnectedAt.Equal(sampleState().ConnectedAt) {
		t.Fatalf("identity fields lost: %+v", saved)
	}
}

func TestDeadRefreshTokenRequiresReconnect(t *testing.T) {
	for _, code := range []string{"bad_refresh_token", "incorrect_client_credentials", "unauthorized_client"} {
		t.Run(code, func(t *testing.T) {
			var hits atomic.Int32
			tk, path := newTestTokens(t, expiringState(), jsonHandler(`{"error":"`+code+`","error_description":"secret detail"}`, &hits))
			_, err := tk.Token(context.Background())
			if !errors.Is(err, ErrReconnect) {
				t.Fatalf("err = %v, want ErrReconnect", err)
			}
			if strings.Contains(err.Error(), "secret detail") || strings.Contains(err.Error(), code) {
				t.Fatalf("error text leaks the response: %v", err)
			}
			if tk.ok {
				t.Fatal("in-memory state must be dropped")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("dead state file must be removed: %v", err)
			}
			if !tk.ReconnectRequired() {
				t.Fatal("ReconnectRequired must be true")
			}
		})
	}
}

func TestExpiredRefreshRemovesState(t *testing.T) {
	var hits atomic.Int32
	st := expiringState()
	st.RefreshExpiresAt = tokenTestNow.Add(-time.Second)
	tk, path := newTestTokens(t, st, jsonHandler(refreshOK, &hits))
	if _, err := tk.Token(context.Background()); !errors.Is(err, ErrReconnect) {
		t.Fatalf("err = %v, want ErrReconnect", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state file must be removed: %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("no request expected for an expired refresh token")
	}
}

func TestTransientRefreshFailureKeepsState(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"5xx":       func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		"ratelimit": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTooManyRequests) },
		"badjson":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
		"notokens":  func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			st := expiringState()
			st.AccessExpiresAt = tokenTestNow.Add(-time.Second) // geçerli jeton yok: hata dönmeli
			tk, path := newTestTokens(t, st, h)
			_, err := tk.Token(context.Background())
			if _, statErr := os.Stat(path); statErr != nil {
				t.Fatalf("transient failure must keep the file: %v", statErr)
			}
			if err == nil || errors.Is(err, ErrReconnect) || tk.ReconnectRequired() {
				t.Fatalf("err = %v, want a transient error", err)
			}
			if !tk.ok || tk.st.RefreshToken != "ghr_old" {
				t.Fatal("old state must be kept")
			}
		})
	}
}

func TestNonExpiringTokenIsNeverRefreshed(t *testing.T) {
	var hits atomic.Int32
	st := sampleState()
	st.AccessExpiresAt = time.Time{}
	tk, _ := newTestTokens(t, st, jsonHandler(refreshOK, &hits))
	got, err := tk.Token(context.Background())
	if err != nil || got != "ghu_old" || hits.Load() != 0 {
		t.Fatalf("Token = %q, %v, hits %d", got, err, hits.Load())
	}
}

func TestFreshTokenIsNotRefreshed(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, sampleState(), jsonHandler(refreshOK, &hits))
	if got, err := tk.Token(context.Background()); err != nil || got != "ghu_old" || hits.Load() != 0 {
		t.Fatalf("Token = %q, %v, hits %d", got, err, hits.Load())
	}
}

func TestRefreshSendsNoClientSecret(t *testing.T) {
	var form url.Values
	var method, path string
	tk, _ := newTestTokens(t, expiringState(), func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = w.Write([]byte(refreshOK))
	})
	if _, err := tk.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/login/oauth/access_token" {
		t.Fatalf("request = %s %s", method, path)
	}
	if _, has := form["client_secret"]; has {
		t.Fatal("client_secret must never be sent")
	}
	if form.Get("grant_type") != "refresh_token" || form.Get("client_id") != "Iv23lizBm3zlCdYH3NQ5" || form.Get("refresh_token") != "ghr_old" || len(form) != 3 {
		t.Fatalf("form = %v", form)
	}
}

func TestClientIDMismatchMeansNotConnected(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, sampleState(), jsonHandler(refreshOK, &hits))
	tk.clientID = "Iv23baska"
	if _, err := tk.Token(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Token err = %v", err)
	}
	if _, err := tk.ForceRefresh(context.Background(), "ghu_old"); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("ForceRefresh err = %v", err)
	}
	if hits.Load() != 0 {
		t.Fatal("no request expected")
	}
}

func TestNoStateMeansNotConnected(t *testing.T) {
	tk := &tokens{clientID: testClientID}
	if _, err := tk.Token(context.Background()); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("err = %v", err)
	}
}

func TestForceRefreshRefreshesFreshToken(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, sampleState(), jsonHandler(refreshOK, &hits))
	got, err := tk.ForceRefresh(context.Background(), "ghu_old")
	if err != nil || got != "ghu_new" || hits.Load() != 1 {
		t.Fatalf("ForceRefresh = %q, %v, hits %d", got, err, hits.Load())
	}
}

func TestRefreshWithoutExpiryFieldsMeansNoExpiry(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, expiringState(), jsonHandler(`{"access_token":"ghu_new","refresh_token":"ghr_new"}`, &hits))
	if _, err := tk.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !tk.st.AccessExpiresAt.IsZero() || !tk.st.RefreshExpiresAt.IsZero() {
		t.Fatalf("expiries = %v / %v, want zero", tk.st.AccessExpiresAt, tk.st.RefreshExpiresAt)
	}
}

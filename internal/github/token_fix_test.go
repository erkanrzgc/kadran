package github

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestUnknownOAuthErrorIsTerminal(t *testing.T) {
	for _, code := range []string{"invalid_grant", "access_denied", "expired_token", "brand_new_code"} {
		t.Run(code, func(t *testing.T) {
			var hits atomic.Int32
			tk, path := newTestTokens(t, expiringState(), jsonHandler(`{"error":"`+code+`"}`, &hits))
			if _, err := tk.Token(context.Background()); !errors.Is(err, ErrReconnect) {
				t.Fatalf("err = %v, want ErrReconnect", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("state file must be removed: %v", err)
			}
		})
	}
}

func TestRefreshUnauthorizedIsTerminal(t *testing.T) {
	tk, path := newTestTokens(t, expiringState(), func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	if _, err := tk.Token(context.Background()); !errors.Is(err, ErrReconnect) {
		t.Fatalf("err = %v, want ErrReconnect", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) || !tk.ReconnectRequired() {
		t.Fatalf("state file must be removed (%v) and reconnect required", err)
	}
}

func TestStartupSweepsStrayTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatal(err)
	}
	stray := filepath.Join(dir, ".state-123.tmp")
	keep := []string{filepath.Join(dir, "other.tmp"), filepath.Join(dir, ".state-x.txt")}
	for _, p := range append(keep, stray) {
		if err := os.WriteFile(p, []byte("ghr_live"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	keepDir := filepath.Join(dir, ".state-dir.tmp")
	if err := os.Mkdir(keepDir, 0o700); err != nil {
		t.Fatal(err)
	}
	tk, err := newTokens(path, testClientID, serverClient(t, nil), func() time.Time { return tokenTestNow })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(stray); !os.IsNotExist(err) {
		t.Fatalf("stray temp file survived: %v", err)
	}
	for _, p := range append(keep, keepDir) {
		if _, err := os.Lstat(p); err != nil {
			t.Fatalf("unrelated entry removed: %v", err)
		}
	}
	if got, err := tk.Token(context.Background()); err != nil || got != "ghu_old" {
		t.Fatalf("Token = %q, %v", got, err)
	}
}

func TestNewTokensCorruptStateMeansNotConnected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	tk, err := newTokens(path, testClientID, serverClient(t, nil), nil)
	if err != nil || tk.ok {
		t.Fatalf("newTokens = ok %v, err %v", tk.ok, err)
	}
}

func TestSaveStateIgnoresDirSyncFailure(t *testing.T) {
	old := syncDirFn
	syncDirFn = func(string) error { return errors.New("sync failed") }
	t.Cleanup(func() { syncDirFn = old })
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatalf("saveState = %v, want nil", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestForceRefreshIsSingleFlightAcross401s(t *testing.T) {
	var hits atomic.Int32
	const callers = 10
	started := newStartLine(callers)
	tk, _ := newTestTokens(t, sampleState(), func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		started.wait(t) // yenileme, bütün çağıranlar yola çıkana dek sürer
		_, _ = w.Write([]byte(refreshOK))
	})
	var wg sync.WaitGroup
	results := make([]string, callers)
	errs := make([]error, callers)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.arrive()
			results[i], errs[i] = tk.ForceRefresh(context.Background(), "ghu_old")
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

func TestForceRefreshDeadTokenRequiresReconnect(t *testing.T) {
	var hits atomic.Int32
	tk, path := newTestTokens(t, sampleState(), jsonHandler(`{"error":"bad_refresh_token"}`, &hits))
	if _, err := tk.ForceRefresh(context.Background(), "ghu_old"); !errors.Is(err, ErrReconnect) {
		t.Fatalf("err = %v, want ErrReconnect", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) || !tk.ReconnectRequired() {
		t.Fatalf("state file must be removed (%v) and reconnect required", err)
	}
}

func TestForceRefreshSkipsWhenAlreadyRefreshed(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, sampleState(), jsonHandler(refreshOK, &hits))
	got, err := tk.ForceRefresh(context.Background(), "ghu_stale")
	if err != nil || got != "ghu_old" || hits.Load() != 0 {
		t.Fatalf("ForceRefresh = %q, %v, hits %d", got, err, hits.Load())
	}
}

func TestTransientFailureServesStillValidToken(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, expiringState(), func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	})
	got, err := tk.Token(context.Background())
	if err != nil || got != "ghu_old" || hits.Load() != 1 {
		t.Fatalf("Token = %q, %v, hits %d; want old token, nil, 1", got, err, hits.Load())
	}
	if tk.ReconnectRequired() {
		t.Fatal("transient failure must not require reconnect")
	}
}

func TestRefreshMarginBoundary(t *testing.T) {
	for name, tc := range map[string]struct {
		left time.Duration
		hits int32
	}{
		"exactly 5 minutes left is not refreshed": {refreshMargin, 0},
		"just under 5 minutes is refreshed":       {refreshMargin - time.Nanosecond, 1},
	} {
		t.Run(name, func(t *testing.T) {
			var hits atomic.Int32
			st := sampleState()
			st.AccessExpiresAt = tokenTestNow.Add(tc.left)
			tk, _ := newTestTokens(t, st, jsonHandler(refreshOK, &hits))
			if _, err := tk.Token(context.Background()); err != nil || hits.Load() != tc.hits {
				t.Fatalf("err %v, hits %d, want %d", err, hits.Load(), tc.hits)
			}
		})
	}
}

func TestExpiryIsCapped(t *testing.T) {
	var hits atomic.Int32
	body := `{"access_token":"ghu_new","expires_in":99999999999,"refresh_token":"ghr_new","refresh_token_expires_in":99999999999}`
	tk, _ := newTestTokens(t, expiringState(), jsonHandler(body, &hits))
	if _, err := tk.Token(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := tokenTestNow.Add(maxExpiry)
	if !tk.st.AccessExpiresAt.Equal(want) || !tk.st.RefreshExpiresAt.Equal(want) {
		t.Fatalf("expiries = %v / %v, want %v", tk.st.AccessExpiresAt, tk.st.RefreshExpiresAt, want)
	}
}

func TestReconnectRequiredStartsFalse(t *testing.T) {
	var hits atomic.Int32
	tk, _ := newTestTokens(t, sampleState(), jsonHandler(refreshOK, &hits))
	if tk.ReconnectRequired() {
		t.Fatal("must start false")
	}
}

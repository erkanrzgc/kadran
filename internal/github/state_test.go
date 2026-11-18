package github

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testClientID = "Iv23lizBm3zlCdYH3NQ5"

func sampleState() state {
	at := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	return state{
		Version: stateVersion, ClientID: testClientID, Login: "octo", UserID: 42,
		AccessToken: "ghu_old", AccessExpiresAt: at.Add(8 * time.Hour),
		RefreshToken: "ghr_old", RefreshExpiresAt: at.Add(180 * 24 * time.Hour), ConnectedAt: at,
	}
}

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	want := sampleState()
	if err := saveState(path, want); err != nil {
		t.Fatal(err)
	}
	got, ok, err := loadState(path, testClientID)
	if err != nil || !ok {
		t.Fatalf("loadState = ok %v, err %v", ok, err)
	}
	if got != want {
		t.Fatalf("state changed in round trip: %+v", got)
	}
}

func TestLoadStateMissingFileIsNotConnected(t *testing.T) {
	_, ok, err := loadState(filepath.Join(t.TempDir(), "yok.json"), testClientID)
	if ok || err != nil {
		t.Fatalf("missing file: ok %v, err %v", ok, err)
	}
}

func TestLoadStateClientIDMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatal(err)
	}
	_, ok, err := loadState(path, "Iv23baska")
	if ok || err != nil {
		t.Fatalf("mismatch: ok %v, err %v", ok, err)
	}
}

func TestLoadStateCorruptReturnsErrorWithoutPanic(t *testing.T) {
	for name, content := range map[string]string{
		"json":    "{not json",
		"version": `{"version":99,"client_id":"` + testClientID + `","access_token":"ghu_x"}`,
		"empty":   `{"version":1,"client_id":"` + testClientID + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := loadState(path, testClientID); ok || err == nil {
				t.Fatalf("corrupt state: ok %v, err %v", ok, err)
			}
		})
	}
}

func TestSaveStateFailureRemovesTempFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	if err := os.Mkdir(target, 0o700); err != nil { // rename dizinin üstüne başarısız olur
		t.Fatal(err)
	}
	if err := saveState(target, sampleState()); err == nil {
		t.Fatal("expected a save error")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("leftover files after failed save: %v, %v", entries, err)
	}
}

func TestRemoveStateIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := saveState(path, sampleState()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := removeState(path); err != nil {
			t.Fatalf("removeState #%d: %v", i, err)
		}
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state file still exists: %v", err)
	}
}

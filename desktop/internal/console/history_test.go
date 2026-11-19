package console

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func assertPerm(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("izin = %o, istenen 600", perm)
	}
}

func TestHistoryCapAndPerms(t *testing.T) {
	h := History{Dir: t.TempDir()}
	for i := 0; i < MaxHistory+1; i++ {
		if err := h.Append("hetzner", fmt.Sprintf("status %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Append("gcp", "ps"); err != nil {
		t.Fatal(err)
	}
	got := h.Load("hetzner")
	if len(got) != MaxHistory {
		t.Fatalf("satır sayısı = %d, istenen %d", len(got), MaxHistory)
	}
	if got[0] != "status 1" || got[len(got)-1] != fmt.Sprintf("status %d", MaxHistory) {
		t.Errorf("eski satır düşmedi: ilk %q son %q", got[0], got[len(got)-1])
	}
	if g := h.Load("gcp"); len(g) != 1 || g[0] != "ps" {
		t.Errorf("sunucular karıştı: %q", g)
	}
	if g := h.Load("yok"); len(g) != 0 {
		t.Errorf("bilinmeyen sunucu boş olmalı: %q", g)
	}
	assertPerm(t, filepath.Join(h.Dir, "console-history.json"))
}

func TestHistoryLoadMissingOrCorrupt(t *testing.T) {
	h := History{Dir: t.TempDir()}
	if got := h.Load("a"); len(got) != 0 {
		t.Errorf("dosya yokken %q", got)
	}
	if err := os.WriteFile(filepath.Join(h.Dir, "console-history.json"), []byte("{bozuk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := h.Load("a"); len(got) != 0 {
		t.Errorf("bozuk dosyada %q", got)
	}
	if err := h.Append("a", "status"); err != nil {
		t.Fatal(err)
	}
	if got := h.Load("a"); len(got) != 1 {
		t.Errorf("bozuk dosyadan sonra yazım: %q", got)
	}
}

func TestHistoryLeavesNoTempFiles(t *testing.T) {
	h := History{Dir: t.TempDir()}
	for i := 0; i < 3; i++ {
		if err := h.Append("a", "x"); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(h.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("dizinde %d dosya var", len(entries))
	}
}

func TestAdminSettingDefaultsOff(t *testing.T) {
	s := AdminSetting{Dir: t.TempDir()}
	if s.Load() {
		t.Error("dosya yokken true")
	}
	if err := os.WriteFile(filepath.Join(s.Dir, "console-admin.json"), []byte("{bozuk"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s.Load() {
		t.Error("bozuk JSON'da true")
	}
}

func TestAdminSettingRoundTrip(t *testing.T) {
	s := AdminSetting{Dir: t.TempDir()}
	if err := s.Save(true); err != nil {
		t.Fatal(err)
	}
	if !s.Load() {
		t.Error("kaydedilen true okunamadı")
	}
	assertPerm(t, filepath.Join(s.Dir, "console-admin.json"))
	if err := s.Save(false); err != nil {
		t.Fatal(err)
	}
	if s.Load() {
		t.Error("kaydedilen false okunamadı")
	}
}

func TestAdminLogAppendsJSONL(t *testing.T) {
	l := AdminLog{Dir: t.TempDir()}
	ts := time.Date(2026, 11, 19, 15, 4, 5, 0, time.FixedZone("TR", 3*3600))
	for i := 0; i < 2; i++ {
		e := AdminEntry{Time: ts, Server: "hetzner", Command: "prune -all", Exit: i, Duration: 1500 * time.Millisecond}
		if err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(l.Dir, "console-admin.log")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("satır sayısı = %d", len(lines))
	}
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &rec); err != nil {
		t.Fatalf("geçersiz JSON: %v", err)
	}
	if rec["time"] != "2026-11-19T12:04:05Z" {
		t.Errorf("time = %v", rec["time"])
	}
	if rec["server"] != "hetzner" || rec["command"] != "prune -all" || rec["exit"] != float64(1) || rec["duration_ms"] != float64(1500) {
		t.Errorf("kayıt = %v", rec)
	}
	assertPerm(t, path)
}

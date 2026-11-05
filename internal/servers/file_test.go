package servers

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func testStore(t *testing.T) Store {
	t.Helper()
	return Store{Path: filepath.Join(t.TempDir(), "kadran", "servers.json")}
}

func addServer(s Server) func([]Server) ([]Server, error) {
	return func(list []Server) ([]Server, error) { return Add(list, s) }
}

func TestLoadMissingIsEmpty(t *testing.T) {
	list, err := testStore(t).Load()
	if err != nil || list != nil {
		t.Fatalf("eksik dosya: %+v, %v", list, err)
	}
}

func TestUpdateRoundTrip(t *testing.T) {
	s := testStore(t)
	for _, srv := range []Server{
		{Name: "hetzner", Target: "kadran-client@203.0.113.10", Admin: "root@203.0.113.10", Provider: "Hetzner"},
		{Name: "gcp", Target: "kadran-client@198.51.100.7", Admin: "ops@198.51.100.7", Sudo: true},
	} {
		if _, err := s.Update(addServer(srv)); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.Load()
	if err != nil || len(list) != 2 || list[0].Name != "hetzner" || list[1].Name != "gcp" || !list[1].Sudo {
		t.Fatalf("geri okunan: %+v, %v", list, err)
	}
	raw, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || f.Version != 1 {
		t.Fatalf("sürüm alanı: %d, %v\n%s", f.Version, err, raw)
	}
	if strings.Contains(string(raw), `"sudo": false`) || strings.Contains(string(raw), `"admin": ""`) {
		t.Errorf("boş alanlar yazılmamalı:\n%s", raw)
	}
}

func TestUpdatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows'ta izin bitleri yok; %AppData% zaten kullanıcıya özel")
	}
	s := testStore(t)
	if _, err := s.Update(addServer(Server{Name: "a", Target: "kadran-client@a"})); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{filepath.Dir(s.Path): 0o700, s.Path: 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s izni %o, beklenen %o", path, got, want)
		}
	}
}

func writeRaw(t *testing.T, s Store, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertUnchanged(t *testing.T, s Store, body string) {
	t.Helper()
	got, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("dosya değişti:\n%s", got)
	}
}

func TestUpdateLeavesDamagedFile(t *testing.T) {
	s := testStore(t)
	body := `{"version":1,"servers":[`
	writeRaw(t, s, body)
	_, err := s.Update(addServer(Server{Name: "a", Target: "kadran-client@a"}))
	if err == nil || !strings.Contains(err.Error(), s.Path) {
		t.Fatalf("bozuk dosyada hata yolu söylemeli: %v", err)
	}
	if _, err := s.Load(); err == nil {
		t.Error("Load bozuk dosyayı boş liste saydı")
	}
	assertUnchanged(t, s, body)
}

func TestNewerFileReadOnly(t *testing.T) {
	s := testStore(t)
	body := `{"version":2,"servers":[{"name":"a","target":"kadran-client@a","future":true}]}`
	writeRaw(t, s, body)
	list, err := s.Load()
	if err != nil || len(list) != 1 || list[0].Name != "a" {
		t.Fatalf("yeni sürüm okunmalı: %+v, %v", list, err)
	}
	if _, err := s.Update(addServer(Server{Name: "b", Target: "kadran-client@b"})); !errors.Is(err, ErrNewerFile) {
		t.Fatalf("yeni sürüme yazıldı: %v", err)
	}
	assertUnchanged(t, s, body)
}

func TestLoadRejectsOptionLikeTarget(t *testing.T) {
	s := testStore(t)
	writeRaw(t, s, `{"version":1,"servers":[{"name":"evil","target":"-oProxyCommand=calc@x"}]}`)
	_, err := s.Load()
	if err == nil || !strings.Contains(err.Error(), "-oProxyCommand") || !strings.Contains(err.Error(), "evil") {
		t.Fatalf("seçenek gibi hedef okundu: %v", err)
	}
}

func TestLoadRejectsMissingVersion(t *testing.T) {
	s := testStore(t)
	writeRaw(t, s, `{"servers":[]}`)
	if _, err := s.Load(); err == nil {
		t.Fatal("sürümsüz dosya kabul edildi")
	}
}

func TestUpdateFnErrorWritesNothing(t *testing.T) {
	s := testStore(t)
	if _, err := s.Update(addServer(Server{Name: "a", Target: "kadran-client@a"})); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if _, err := s.Update(func([]Server) ([]Server, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("fn hatası dönmedi: %v", err)
	}
	if _, err := s.Update(func(l []Server) ([]Server, error) {
		return append(l, Server{Name: "-bad", Target: "kadran-client@b"}), nil
	}); err == nil {
		t.Fatal("geçersiz liste yazıldı")
	}
	assertUnchanged(t, s, string(before))
	if entries, _ := os.ReadDir(filepath.Dir(s.Path)); len(entries) != 1 {
		t.Errorf("geçici dosya kaldı: %v", entries)
	}
}

func TestConcurrentUpdatesStayValid(t *testing.T) {
	s := testStore(t)
	var wg sync.WaitGroup
	errs := make(chan error, 80)
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 10 {
				id := strconv.Itoa(g) + "-" + strconv.Itoa(i)
				if _, err := s.Update(addServer(Server{Name: "s" + id, Target: "kadran-client@h" + id})); err != nil {
					errs <- err
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("eşzamanlı yazma: %v", err)
	}
	list, err := s.Load()
	if err != nil || len(list) == 0 {
		t.Fatalf("son dosya okunamadı: %d, %v", len(list), err)
	}
}

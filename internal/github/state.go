package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

const (
	stateVersion = 1
	// maxStateFile durum dosyası için üst sınırdır (okuma tarafı da aynı sınırı uygular).
	maxStateFile = 64 << 10
)

// state diskteki bağlantı durumudur. Dosya 0600 olmalı; içindeki jetonlar
// hiçbir yerde günlüğe ya da hata metnine girmez.
type state struct {
	Version          int       `json:"version"`
	ClientID         string    `json:"client_id"`
	Login            string    `json:"login"`
	UserID           int64     `json:"user_id"`
	AccessToken      string    `json:"access_token"`
	AccessExpiresAt  time.Time `json:"access_expires_at"` // sıfır = süresiz
	RefreshToken     string    `json:"refresh_token"`
	RefreshExpiresAt time.Time `json:"refresh_expires_at"`
	ConnectedAt      time.Time `json:"connected_at"`
}

var (
	errStateCorrupt  = errors.New("github: state file is corrupt")
	errStateVersion  = errors.New("github: unsupported state file version")
	errStateTooLarge = errors.New("github: state file too large or unreadable")
)

// loadState durum dosyasını okur. Bağlı değilse (dosya yok ya da client_id
// uyuşmuyor) (state{}, false, nil) döner; bozuk ya da güvensiz dosya için hata
// döner. Günlüğe yazmak çağıranın işidir.
func loadState(path, clientID string) (state, bool, error) {
	data, err := readStateFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return state{}, false, nil
	}
	if err != nil {
		return state{}, false, err
	}
	var s state
	if json.Unmarshal(data, &s) != nil {
		return state{}, false, errStateCorrupt
	}
	if s.Version != stateVersion {
		return state{}, false, errStateVersion
	}
	if s.AccessToken == "" || s.ClientID == "" {
		return state{}, false, errStateCorrupt
	}
	if s.ClientID != clientID {
		return state{}, false, nil
	}
	return s, true, nil
}

// syncDirFn testlerde dizin fsync hatasını zorlamak için kancadır.
var syncDirFn = syncDir

// saveState durumu atomik yazar: aynı dizinde geçici dosya (0600, gizli veri
// yazılmadan önce), fsync, rename, dizin fsync. Hata olursa geçici dosya silinir.
func saveState(path string, s state) (err error) {
	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("github: encoding state: %w", err)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".state-*.tmp")
	if err != nil {
		return fmt.Errorf("github: creating state file: %w", err)
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err = tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("github: securing state file: %w", err)
	}
	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("github: writing state file: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("github: syncing state file: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("github: closing state file: %w", err)
	}
	if err = os.Rename(name, path); err != nil {
		return fmt.Errorf("github: replacing state file: %w", err)
	}
	// Yeni çift rename ile dosyada; dizin fsync hatası bağlantıyı düşürmemeli.
	if syncDirFn(dir) != nil {
		slog.Warn("github state directory could not be synced")
	}
	return nil
}

// sweepStrayTemps dizindeki artık `.state-*.tmp` normal dosyalarını siler;
// sembolik bağlara ve dizinlere dokunmaz, başka desene bakmaz.
func sweepStrayTemps(dir string) error {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("github: scanning state directory: %w", err)
	}
	var errs []error
	for _, e := range entries {
		if ok, _ := filepath.Match(".state-*.tmp", e.Name()); !ok || !e.Type().IsRegular() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("github: removing stray state files: %w", errors.Join(errs...))
	}
	return nil
}

// removeState durum dosyasını siler; dosya zaten yoksa hata değildir.
func removeState(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("github: removing state file: %w", err)
	}
	if err := syncDir(filepath.Dir(path)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("github: syncing state directory: %w", err)
	}
	return nil
}

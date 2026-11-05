package servers

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
)

// maxFileSize, okunacak dosyanın üst sınırı. 100 kayıt birkaç on KB; daha
// büyüğü bu dosya değildir.
const maxFileSize = 1 << 20

// DefaultPath, kullanıcının yapılandırma dizinindeki kadran/servers.json;
// TOTP oturumlarının (sessions/) yanı.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not find the user config directory: %w", err)
	}
	return filepath.Join(base, "kadran", "servers.json"), nil
}

// Store, listenin dosyasıdır. Path boşsa DefaultPath kullanılır; testler
// geçici bir yol verir.
type Store struct {
	Path string
}

type fileBody struct {
	Version int      `json:"version"`
	Servers []Server `json:"servers"`
}

func (s Store) path() (string, error) {
	if s.Path != "" {
		return s.Path, nil
	}
	return DefaultPath()
}

// Load, listeyi okur. Dosya yoksa liste boştur, hata değil. Bozuk ya da
// geçersiz kayıt taşıyan dosya hatadır ve yolu söylenir.
func (s Store) Load() ([]Server, error) {
	list, _, _, err := s.load()
	return list, err
}

func (s Store) load() (list []Server, version int, path string, err error) {
	path, err = s.path()
	if err != nil {
		return nil, 0, "", err
	}
	raw, err := readRetry(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, FileVersion, path, nil
	}
	if err != nil {
		return nil, 0, path, fmt.Errorf("could not read the server list (%s): %w", path, err)
	}
	if len(raw) > maxFileSize {
		return nil, 0, path, fmt.Errorf("the server list (%s) is larger than %d bytes", path, maxFileSize)
	}
	var body fileBody
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, 0, path, fmt.Errorf("the server list is damaged (%s): %w — fix or remove the file", path, err)
	}
	if body.Version < 1 {
		return nil, 0, path, fmt.Errorf("the server list (%s) has no version — fix or remove the file", path)
	}
	if err := Validate(body.Servers); err != nil {
		return nil, 0, path, fmt.Errorf("the server list (%s) has an invalid entry: %w", path, err)
	}
	return body.Servers, body.Version, path, nil
}

// Update, listeyi okur, fn ile değiştirir, denetler ve atomik yazar. fn ya da
// denetim hata verirse dosyaya dokunulmaz. Okumayla yazma arasında başka bir
// süreç yazarsa son yazan kazanır (K-162); dosya hiçbir anda yarım olmaz.
func (s Store) Update(fn func([]Server) ([]Server, error)) ([]Server, error) {
	list, version, path, err := s.load()
	if err != nil {
		return nil, err
	}
	if version > FileVersion {
		return nil, fmt.Errorf("%w (%s, version %d)", ErrNewerFile, path, version)
	}
	next, err := fn(slices.Clone(list))
	if err != nil {
		return nil, err
	}
	if err := Validate(next); err != nil {
		return nil, err
	}
	if err := writeFile(path, next); err != nil {
		return nil, err
	}
	return next, nil
}

// writeFile, aynı dizindeki geçici dosyaya yazar, diske indirir ve rename
// eder: okuyan hiçbir süreç yarım bir dosya görmez.
func writeFile(path string, list []Server) (err error) {
	if list == nil {
		list = []Server{}
	}
	data, err := json.MarshalIndent(fileBody{Version: FileVersion, Servers: list}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".servers-*.json")
	if err != nil {
		return fmt.Errorf("could not write the server list (%s): %w", path, err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("could not write the server list (%s): %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("could not write the server list (%s): %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("could not write the server list (%s): %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("could not write the server list (%s): %w", path, err)
	}
	if err := renameRetry(tmp.Name(), path); err != nil {
		return fmt.Errorf("could not write the server list (%s): %w", path, err)
	}
	return nil
}

// withRetry: Windows'ta dosya o an başka bir süreçte (CLI ya da masaüstü)
// okunuyor ya da değiştiriliyorsa hem okuma hem rename düşebiliyor. Kısa ve
// artan aralıklarla yeniden denenir (toplam ~0,5 sn), sonra açık hata
// (K-162). POSIX'te yeniden denenecek hata yok (retryable).
func withRetry(fn func() error) error {
	const attempts = 10
	var err error
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil || !retryable(err) {
			return err
		}
		time.Sleep(time.Duration(i) * 10 * time.Millisecond)
	}
	return err
}

func readRetry(path string) ([]byte, error) {
	var raw []byte
	err := withRetry(func() (err error) {
		raw, err = os.ReadFile(path)
		return err
	})
	return raw, err
}

func renameRetry(from, to string) error {
	return withRetry(func() error { return os.Rename(from, to) })
}

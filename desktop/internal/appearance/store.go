package appearance

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// FileName, ayar dosyasının adı (kullanıcı verisi dizininde).
const FileName = "appearance.json"

// Store, ayar dosyası. Dizin Electron'un kullandığıyla aynı
// (%APPDATA%\kadran-desktop): taşınınca ayarlar kaybolmasın (K-165).
type Store struct {
	Dir string
}

func (s Store) path() string { return filepath.Join(s.Dir, FileName) }

// Load, ayarları okur. Dosya yoksa varsayılan, sessizce; okunamaz ya da
// çözümlenemezse varsayılan, uyarıyla. Okunan her değer Sanitize'dan geçer.
func (s Store) Load(warn func(string)) Appearance {
	raw, err := os.ReadFile(s.path())
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) && warn != nil {
			warn(fmt.Sprintf("could not read the appearance settings: %v", err))
		}
		return Defaults()
	}
	if !json.Valid(raw) {
		if warn != nil {
			warn("could not parse the appearance file, using the defaults")
		}
		return Defaults()
	}
	return Sanitize(raw)
}

// Save, ayarları doğrulayıp ATOMİK yazar: önce geçici dosyaya, sonra
// yeniden adlandırma. Yarıda kesilen bir yazım yarım bir JSON bırakmaz; en
// kötü durumda eski dosya kalır. Yazılan (temizlenmiş) değeri döner.
func (s Store) Save(raw json.RawMessage) (Appearance, error) {
	clean := Sanitize(raw)
	var body bytes.Buffer
	enc := json.NewEncoder(&body)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(clean); err != nil {
		return Appearance{}, err
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return Appearance{}, fmt.Errorf("could not create the settings directory: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.%d.tmp", s.path(), os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, body.Bytes(), 0o600); err != nil {
		_ = os.Remove(tmp)
		return Appearance{}, fmt.Errorf("could not save the appearance settings: %w", err)
	}
	if err := os.Rename(tmp, s.path()); err != nil {
		_ = os.Remove(tmp)
		return Appearance{}, fmt.Errorf("could not save the appearance settings: %w", err)
	}
	return clean, nil
}

package console

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const (
	// MaxHistory, sunucu başına saklanan en çok satır sayısı.
	MaxHistory  = 500
	historyFile = "console-history.json"
	fileMode    = 0o600
	dirMode     = 0o700
)

// fileMu, bu paketin dosya yazımlarını (okuma-değiştirme-yazma) sıralar.
var fileMu sync.Mutex

// History, sunucu başına konsol geçmişini Dir altında tutar.
type History struct{ Dir string }

// Load, sunucunun geçmiş satırlarını döner (eskiden yeniye). Dosya yok ya da
// bozuksa boş döner.
func (h History) Load(server string) []string {
	fileMu.Lock()
	defer fileMu.Unlock()
	return h.read()[server]
}

// Append, satırı sunucunun geçmişine ekler; en çok MaxHistory satır kalır,
// en eskisi düşer. Dosya geçici dosya + yeniden adlandırma ile yazılır.
func (h History) Append(server, line string) error {
	fileMu.Lock()
	defer fileMu.Unlock()
	all := h.read()
	lines := append(all[server], line)
	if len(lines) > MaxHistory {
		lines = lines[len(lines)-MaxHistory:]
	}
	all[server] = lines
	data, err := json.Marshal(all)
	if err != nil {
		return fmt.Errorf("encode console history: %w", err)
	}
	return writeFileAtomic(filepath.Join(h.Dir, historyFile), data)
}

// read, dosyayı okur; yok ya da bozuksa boş eşleme döner (asla nil).
func (h History) read() map[string][]string {
	all := map[string][]string{}
	raw, err := os.ReadFile(filepath.Join(h.Dir, historyFile))
	if err != nil || json.Unmarshal(raw, &all) != nil || all == nil {
		return map[string][]string{}
	}
	return all
}

// writeFileAtomic, veriyi aynı dizinde geçici dosyaya yazıp hedefin üstüne
// yeniden adlandırır; yarım dosya görünmez. İzin 0600.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpName := tmp.Name()
	if err := writeAndClose(tmp, data); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, fileMode); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", f.Name(), err)
	}
	return nil
}

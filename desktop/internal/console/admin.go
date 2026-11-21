package console

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	adminSettingFile = "console-admin.json"
	adminLogFile     = "console-admin.log"
)

// AdminSetting, "yönetici kipi" açık mı bilgisini Dir altında tutar.
type AdminSetting struct{ Dir string }

type adminSettingDoc struct {
	On bool `json:"on"`
}

// Load, ayarı okur; dosya yok ya da okunamıyorsa false (varsayılan kapalı).
func (s AdminSetting) Load() bool {
	fileMu.Lock()
	defer fileMu.Unlock()
	raw, err := os.ReadFile(filepath.Join(s.Dir, adminSettingFile))
	if err != nil {
		return false
	}
	var doc adminSettingDoc
	if json.Unmarshal(raw, &doc) != nil {
		return false
	}
	return doc.On
}

// Save, ayarı geçici dosya + yeniden adlandırma ile yazar.
func (s AdminSetting) Save(on bool) error {
	data, err := json.Marshal(adminSettingDoc{On: on})
	if err != nil {
		return fmt.Errorf("encode admin setting: %w", err)
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	return writeFileAtomic(filepath.Join(s.Dir, adminSettingFile), data)
}

// AdminEntry, yönetici kipinde çalışan bir komutun kaydı.
type AdminEntry struct {
	Time     time.Time
	Server   string
	Command  string
	Exit     int
	Duration time.Duration
}

// AdminLog, yönetici kipi komutlarını yalnız-ekleme bir JSON satırı dosyasına
// yazar.
type AdminLog struct{ Dir string }

type adminLogRecord struct {
	Time       string `json:"time"`
	Server     string `json:"server"`
	Command    string `json:"command"`
	Exit       int    `json:"exit"`
	DurationMS int64  `json:"duration_ms"`
}

// Append, kaydı dosyanın sonuna tek JSON satırı olarak ekler (zaman UTC RFC3339).
func (l AdminLog) Append(e AdminEntry) error {
	data, err := json.Marshal(adminLogRecord{
		Time:       e.Time.UTC().Format(time.RFC3339),
		Server:     e.Server,
		Command:    e.Command,
		Exit:       e.Exit,
		DurationMS: e.Duration.Milliseconds(),
	})
	if err != nil {
		return fmt.Errorf("encode admin log entry: %w", err)
	}
	if err := os.MkdirAll(l.Dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", l.Dir, err)
	}
	fileMu.Lock()
	defer fileMu.Unlock()
	path := filepath.Join(l.Dir, adminLogFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

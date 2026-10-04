// Package vaultkey, kasa anahtarını üretir (K-123).
//
// Yalnız kadran-vault ikilisi içe aktarıyor; kurulum onu root olarak bir
// kez çağırıyor. Executor'da değil, çünkü executor'ın her satırı
// ayrıcalıklı yüzeye sayılıyor ve bu kod daemon'dan hiçbir girdi almıyor.
// kadrand'de değil, çünkü kadrand root koşmamalı.
package vaultkey

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"filippo.io/age"
)

// Init, anahtar yoksa üretir ve AÇIK anahtarı out'a yazar. Anahtar varsa
// ona dokunmaz, yalnız açık anahtarı yazar. Var olan dosya bozuksa ya da
// başkalarına açıksa durur: üstüne yenisini yazmak var olan bütün mühürlü
// değerleri çözülemez kılardı. Özel anahtar out'a yazılmaz.
func Init(path string, out io.Writer) error {
	id, err := load(path)
	if errors.Is(err, fs.ErrNotExist) {
		id, err = create(path)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, id.Recipient().String())
	return err
}

func load(path string) (*age.X25519Identity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("kasa anahtarı okunamadı: %w", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("kasa anahtarı %s başkalarına açık (%v), 0600 olmalı", path, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("kasa anahtarı okunamadı: %w", err)
	}
	// age'in hatası anahtarın parçalarını taşıyabilir; ekrana gitmesin.
	id, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("kasa anahtarı %s geçersiz (X25519 olmalı); üstüne yazılmadı", path)
	}
	return id, nil
}

// create, anahtarı O_EXCL ile yazar: arada başka biri dosyayı yarattıysa
// onun üstüne yazmaz. Diske kalıcılığı kurulum `sync` ile sağlıyor.
func create(path string) (*age.X25519Identity, error) {
	id, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("kasa anahtarı üretilemedi: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("kasa anahtarı yazılamadı: %w", err)
	}
	if _, err := fmt.Fprintln(f, id.String()); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("kasa anahtarı yazılamadı: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("kasa anahtarı yazılamadı: %w", err)
	}
	return id, nil
}

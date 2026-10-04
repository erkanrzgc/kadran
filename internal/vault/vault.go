// Package vault, uygulama ortam değişkenlerinin değerlerini executor'ın
// açık anahtarıyla şifreler (K-123).
//
// Daemon yalnız AÇIK anahtarı tutar: değerleri yazarken şifreler, okuyamaz.
// Çözme yalnız executor'da, root'un anahtarıyla, konteyner kurulurken
// yapılır. Bu paket ayrıcalıklı yüzeye girmez; kadran-exec onu içe
// aktarmıyor, biçimi (önek ve bağ) kendi kodunda ayrıca tanımlıyor.
package vault

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
)

// Prefix, mühürlü bir değerin önekidir. Gerisi base64(age ikili biçimi).
const Prefix = "age:"

// maxRecipientFile, alıcı dosyasından okunacak en çok bayt. Bir X25519
// alıcısı 62 karakter.
const maxRecipientFile = 1 << 10

// Sealer, değerleri tek bir X25519 alıcısına mühürler.
type Sealer struct {
	r *age.X25519Recipient
}

// NewSealer, `age1…` biçimindeki alıcıdan bir Sealer kurar. Yalnız X25519
// kabul edilir: executor da yalnız X25519 kimliği çözüyor.
func NewSealer(recipient string) (*Sealer, error) {
	r, err := age.ParseX25519Recipient(strings.TrimSpace(recipient))
	if err != nil {
		return nil, fmt.Errorf("kasa alıcısı geçersiz: %w", err)
	}
	return &Sealer{r: r}, nil
}

// LoadSealer, alıcıyı dosyadan okur (kurulumun yazdığı /etc/kadran/vault.pub).
func LoadSealer(path string) (*Sealer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("kasa alıcısı okunamadı: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRecipientFile+1))
	if err != nil {
		return nil, fmt.Errorf("kasa alıcısı okunamadı: %w", err)
	}
	if len(data) > maxRecipientFile {
		return nil, fmt.Errorf("kasa alıcısı dosyası çok büyük: %s", path)
	}
	return NewSealer(string(data))
}

// Recipient, alıcının `age1…` biçimidir.
func (s *Sealer) Recipient() string { return s.r.String() }

// Seal, değeri uygulamaya ve ada BAĞLAYARAK mühürler: şifrelenen metin
// `<appID> NUL <ad> NUL <değer>`. Executor açarken uygulamayı ve adı
// istekteki karşılıklarıyla karşılaştırıyor; böylece bir değer başka bir
// ada ya da uygulamaya kopyalanınca açılmıyor.
func (s *Sealer) Seal(appID, key, value string) (string, error) {
	var buf bytes.Buffer
	w, err := age.Encrypt(&buf, s.r)
	if err != nil {
		return "", fmt.Errorf("%s mühürlenemedi: %w", key, err)
	}
	if _, err := io.WriteString(w, appID+"\x00"+key+"\x00"+value); err != nil {
		return "", fmt.Errorf("%s mühürlenemedi: %w", key, err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("%s mühürlenemedi: %w", key, err)
	}
	return Prefix + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// SealEnv, haritadaki BÜTÜN değerleri (boş olanlar dahil) mühürler ve yeni
// bir harita döndürür; girdiye dokunmaz.
func (s *Sealer) SealEnv(appID string, env map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(env))
	for k, v := range env {
		sealed, err := s.Seal(appID, k, v)
		if err != nil {
			return nil, err
		}
		out[k] = sealed
	}
	return out, nil
}

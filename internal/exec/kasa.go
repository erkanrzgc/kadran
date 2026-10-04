package exec

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"filippo.io/age"
)

// Kasa (K-123): daemon ortam değişkeni değerlerini executor'ın AÇIK
// anahtarıyla mühürlüyor; burada, konteyner kurulurken açılıyor. Biçim
// daemon'daki internal/vault ile aynı; o paket içe aktarılmıyor, çünkü
// buradaki her satır ayrıcalıklı yüzeye sayılıyor.

// sealPrefix, mühürlü değerin öneki. Gerisi base64(age ikili biçimi).
const sealPrefix = "age:"

const (
	// maxSealedValue, tek mühürlü değerin base64 hâlinin üst sınırı. Base64
	// çözülmeden ÖNCE denetleniyor; düz metin şifreli metinden kısa olduğu
	// için çözülen değer de bununla sınırlı. 32 KiB'lık değer ~44 KiB.
	maxSealedValue = 64 << 10
	// maxSealedEnvBytes, mühürlü haritanın toplamı. Her değere ~270 bayt
	// başlık ve bağ ekleniyor, base64 4/3 büyütüyor.
	maxSealedEnvBytes = 192 << 10
)

// LoadVaultIdentity, kasa anahtarını okur. Yalnız X25519 kabul edilir;
// dosya root dışındakilere açıksa reddedilir.
func LoadVaultIdentity(path string) (*age.X25519Identity, error) {
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
	// age'in hatası anahtarın parçalarını taşıyabilir; journal'a gitmesin.
	id, err := age.ParseX25519Identity(strings.TrimSpace(string(data)))
	if err != nil {
		return nil, fmt.Errorf("kasa anahtarı geçersiz (X25519 olmalı): %s", path)
	}
	return id, nil
}

// openEnv, mühürlü haritayı açar. Her değer mühürlü olmalı (kasa zorunlu),
// açılan metin bu uygulamaya ve bu ada bağlı olmalı; açılan harita olağan
// doğrulamadan geçmeli.
func openEnv(id age.Identity, appID string, sealed map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(sealed))
	for k, v := range sealed {
		b64, ok := strings.CutPrefix(v, sealPrefix)
		if !ok {
			return nil, fmt.Errorf("env %q mühürlü değil (kasa zorunlu)", k)
		}
		if len(b64) > maxSealedValue {
			return nil, fmt.Errorf("env %q mühürlü değeri çok büyük", k)
		}
		raw, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, fmt.Errorf("env %q mühürlü değeri bozuk", k)
		}
		r, err := age.Decrypt(bytes.NewReader(raw), id)
		if err != nil {
			return nil, fmt.Errorf("env %q açılamadı: %w", k, err)
		}
		plain, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("env %q açılamadı: %w", k, err)
		}
		val, ok := strings.CutPrefix(string(plain), appID+"\x00"+k+"\x00")
		if !ok {
			return nil, fmt.Errorf("env %q başka bir uygulamaya ya da ada mühürlenmiş", k)
		}
		out[k] = val
	}
	return out, validateEnv(out, maxEnvBytes)
}

// Package sockets, Panely'nin unix soketi dinleyicilerini tutarlı ve
// güvenli biçimde oluşturur.
package sockets

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

// ListenOptions, unix soketi dinleyicisini yapılandırır.
type ListenOptions struct {
	// Path, soket dosyasının yolu.
	Path string

	// Mode, sokete uygulanacak izinler (örn. 0o660).
	Mode os.FileMode

	// GID, sokete atanacak grup. Sıfır veya negatifse değiştirilmez.
	GID int
}

// Listen, unix soketi dinleyicisi oluşturur ve izinlerini ayarlar.
//
// # Üç katmanlı koruma
//
// Soket izinleri tek başına yeterli değildir ve buradaki kod bunu
// varsaymaz. Erişim üç bağımsız katmanla kısıtlanır:
//
//  1. DİZİN izinleri — /run/panely-exec 0750 root:panely'dir. Dizini
//     traverse edemeyen bir süreç içindeki sokete hiç ulaşamaz. Asıl
//     kapı budur ve systemd-tmpfiles tarafından kurulur.
//
//  2. SOKET izinleri — burada ayarlanır. İkinci bir savunma hattı.
//
//  3. SO_PEERCRED — bağlantı başına çekirdekten doğrulama
//     (internal/peercred). Dosya izinleri yanlış kurulmuş olsa bile
//     yetkisiz çağıran reddedilir.
//
// Listen ile Chmod arasında soketin kısa süreliğine umask izinleriyle var
// olduğu bir pencere vardır. Bu pencere 1. ve 3. katmanlar sayesinde
// sömürülebilir değildir: dizin zaten kapalıdır ve bağlantı kurulsa bile
// SO_PEERCRED reddeder.
func Listen(opts ListenOptions) (net.Listener, error) {
	if opts.Path == "" {
		return nil, fmt.Errorf("sockets: yol boş olamaz")
	}
	if opts.Mode == 0 {
		return nil, fmt.Errorf("sockets: mod belirtilmeli")
	}

	// Temiz kapanmayan bir önceki süreçten kalan soket dosyası
	// "address already in use" verir. Dizin systemd tarafından
	// korunduğu için buradaki dosyanın bize ait olduğu güvenlidir.
	if err := removeStaleSocket(opts.Path); err != nil {
		return nil, err
	}

	// noctx bastırılıyor. Önerdiği (*net.ListenConfig).Listen biçimi bir
	// context ister; bu da Listen()'in imzasını değiştirip her çağıranı
	// dolaşmayı gerektirirdi. Karşılığında elde edilen davranış farkı
	// SIFIR: unix soketi dinlemeye almak yerel ve anlık bir işlem, iptal
	// edilecek bir bekleme yok. İmza değişikliği bedelinin karşılığı yok.
	ln, err := net.Listen("unix", opts.Path) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("sockets: %s dinlenemedi: %w", opts.Path, err)
	}

	if err := os.Chmod(opts.Path, opts.Mode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("sockets: izinler ayarlanamadı: %w", err)
	}

	if opts.GID > 0 {
		// -1 = kullanıcıyı değiştirme.
		if err := os.Chown(opts.Path, -1, opts.GID); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("sockets: grup ayarlanamadı: %w", err)
		}
	}

	return ln, nil
}

// removeStaleSocket, yoldaki dosya bir soketse siler.
//
// Soket OLMAYAN bir dosya asla silinmez: yanlış yapılandırılmış bir yol
// yüzünden veritabanının veya bir yapılandırma dosyasının silinmesi kabul
// edilemez. Böyle bir durumda hata döner ve süreç başlamaz.
func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sockets: %s incelenemedi: %w", path, err)
	}

	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf(
			"sockets: %s bir soket değil (%s) — silmeyi reddediyorum",
			path, info.Mode().Type())
	}

	if err := os.Remove(path); err != nil {
		return fmt.Errorf("sockets: eski soket silinemedi: %w", err)
	}
	return nil
}

// EnsureParentDir, soketin bulunacağı dizinin var olduğunu doğrular.
//
// Dizini OLUŞTURMAZ. Üretimde dizinler systemd-tmpfiles tarafından kesin
// sahiplik ve izinlerle kurulur; burada oluşturmak o kesinliği bozar ve
// yanlış sahiplikli bir dizinin sessizce kabul edilmesine yol açardı.
func EnsureParentDir(socketPath string) error {
	dir := filepath.Dir(socketPath)
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf(
			"sockets: %s dizini yok (systemd-tmpfiles kurulumu eksik olabilir): %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("sockets: %s bir dizin değil", dir)
	}
	return nil
}

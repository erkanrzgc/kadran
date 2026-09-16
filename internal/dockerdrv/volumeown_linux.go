//go:build linux

package dockerdrv

import "os"

// platformChownDir, hacim dizinini uygulamanın kullanıcısına devreder.
//
// Grup da aynı kimliğe ayarlanıyor: hacim tek bir uygulamaya ait ve
// paylaşılmıyor, dolayısıyla ayrı bir grup kimliği taşımanın taşıyacağı
// bir bilgi yok.
func platformChownDir(path string, uid int) error {
	return os.Chown(path, uid, uid)
}

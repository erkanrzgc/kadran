//go:build linux

package exec

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// setUmask, süreç umask'ını değiştirir ve eskisini döndürür.
//
// umask süreç genelindedir. Bu paketteki hiçbir test t.Parallel()
// çağırmıyor, bu yüzden sıralı çalışıyorlar ve geri alma güvenli.
func setUmask(mask int) int { return syscall.Umask(mask) }

// TestOpenJournalEnforcesModeOnExistingFile, günlüğün izinlerinin HER
// açılışta yerine oturtulduğunu doğrular — yalnızca dosya yeni
// oluşturulduğunda değil.
//
// # Neden önemli?
//
// 0640, grubun ve diğerlerinin günlüğe YAZAMAMASINI sağlar. Dosya bir
// kez gevşek izinlerle var olduysa (yedekten geri alındı, elle
// oluşturuldu) eski kod onu bir daha düzeltmiyordu ve sonuç SESSİZ
// oluyordu.
//
// ⚠ Bu yorum önceden izni "panelyd'nin okuyup çapraz doğrulama yapması"
// ile gerekçelendiriyordu. Çapraz doğrulama yok (K-079) ve günlük artık
// panelyd'nin giremediği bir dizinde (K-102).
//
// Chown burada sınanamaz (root gerektirir); sınanan chmod yoludur.
// GroupGID sıfır verilerek chown atlanıyor.
func TestOpenJournalEnforcesModeOnExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exec-audit.log")

	// Yanlış izinlerle önceden var olan bir günlük: grup okuyamaz.
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	j, err := OpenJournal(JournalOptions{Path: path})
	if err != nil {
		t.Fatalf("günlük açılamadı: %v", err)
	}
	defer func() { _ = j.Close() }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mod %04o, beklenen 0640 — gevşek kip düzeltilmedi, "+
			"grup ya da diğerleri günlüğe erişebilir", got)
	}
}

// TestOpenJournalCreatesWithReadableMode, yeni oluşturulan günlüğün de
// 0640 olduğunu doğrular. Süreç umask'ı 0640'ı kısabilir; açık chmod
// bu bağımlılığı kaldırıyor.
func TestOpenJournalCreatesWithReadableMode(t *testing.T) {
	old := setUmask(0o077) // grup bitlerini kırpan agresif umask
	defer setUmask(old)

	path := filepath.Join(t.TempDir(), "yeni.log")

	j, err := OpenJournal(JournalOptions{Path: path})
	if err != nil {
		t.Fatalf("günlük açılamadı: %v", err)
	}
	defer func() { _ = j.Close() }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Errorf("mod %04o, beklenen 0640 — umask kırpmış ve düzeltilmemiş", got)
	}
}

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
// Günlüğün 0640 root:panely olmasının tek amacı panelyd'nin onu
// OKUYABİLMESİ; çapraz doğrulama buna dayanıyor. Dosya bir kez yanlış
// izinlerle var olduysa, eski kod onu bir daha düzeltmiyordu ve sonuç
// SESSİZ oluyordu: executor açılır, kayıt yazmaya devam eder, ama
// panelyd okuyamadığı için "ele geçirilmiş panelyd kayıt düşüremez"
// iddiası kimse fark etmeden geçersizleşir.
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
		t.Errorf("mod %04o, beklenen 0640 — panelyd günlüğü okuyamaz "+
			"ve çapraz doğrulama sessizce çalışmaz", got)
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

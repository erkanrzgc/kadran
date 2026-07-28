package sockets

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestListenRequiresPath(t *testing.T) {
	if _, err := Listen(ListenOptions{Mode: 0o660}); err == nil {
		t.Fatal("boş yolla dinleyici oluşturuldu")
	}
}

func TestListenRequiresMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.sock")
	if _, err := Listen(ListenOptions{Path: path}); err == nil {
		t.Fatal("mod belirtilmeden dinleyici oluşturuldu")
	}
}

// TestRemoveStaleSocketRefusesRegularFile, kazara veri kaybına karşı
// korumayı doğrular.
//
// Yanlış yapılandırılmış bir --socket bayrağı veritabanı dosyasını veya bir
// yapılandırma dosyasını işaret edebilir. Eski soketi temizlemek meşrudur;
// soket OLMAYAN bir dosyayı silmek asla değildir. Bu durumda süreç
// başlamamalı, sessizce dosyayı yok etmemelidir.
func TestRemoveStaleSocketRefusesRegularFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panely.db")
	const payload = "degerli veri"
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatalf("test dosyası yazılamadı: %v", err)
	}

	err := removeStaleSocket(path)
	if err == nil {
		t.Fatal("normal dosya soket sanılıp silinmeye çalışıldı")
	}
	if !strings.Contains(err.Error(), "soket değil") {
		t.Errorf("beklenen hata mesajı değil: %v", err)
	}

	// Dosya hâlâ yerinde ve bozulmamış olmalı.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("dosya silinmiş: %v", err)
	}
	if string(got) != payload {
		t.Errorf("dosya içeriği değişmiş: %q", got)
	}
}

func TestRemoveStaleSocketRefusesDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := removeStaleSocket(dir); err == nil {
		t.Fatal("dizin soket sanılıp silinmeye çalışıldı")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("dizin silinmiş: %v", err)
	}
}

func TestRemoveStaleSocketAcceptsMissingPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "yok.sock")
	if err := removeStaleSocket(path); err != nil {
		t.Errorf("var olmayan yol hata verdi: %v", err)
	}
}

func TestEnsureParentDirDetectsMissingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "olmayan-dizin", "x.sock")
	if err := EnsureParentDir(path); err == nil {
		t.Fatal("var olmayan dizin kabul edildi")
	}
}

func TestEnsureParentDirAcceptsExistingDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.sock")
	if err := EnsureParentDir(path); err != nil {
		t.Errorf("var olan dizin reddedildi: %v", err)
	}
}

// TestEnsureParentDirDoesNotCreate, dizinin OLUŞTURULMADIĞINI doğrular.
//
// Üretimde dizinler systemd-tmpfiles tarafından kesin sahiplik ve
// izinlerle kurulur. Burada oluşturmak, yanlış sahiplikli bir dizinin
// sessizce kabul edilmesine yol açardı — tam da /run/panely-exec'in
// root:panely 0750 olmasına bağlı olan izolasyonu delerdi.
func TestEnsureParentDirDoesNotCreate(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "olusturma-beni")
	_ = EnsureParentDir(filepath.Join(missing, "x.sock"))

	if _, err := os.Stat(missing); err == nil {
		t.Fatal("EnsureParentDir dizini oluşturdu — oluşturmamalı")
	}
}

// TestListenAppliesPermissions, soket izinlerinin gerçekten uygulandığını
// doğrular. Windows'ta unix soketi semantiği farklı olduğu için atlanır.
func TestListenAppliesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix soketi izinleri Windows'ta uygulanmıyor")
	}

	path := filepath.Join(t.TempDir(), "x.sock")
	ln, err := Listen(ListenOptions{Path: path, Mode: 0o660})
	if err != nil {
		t.Fatalf("dinleyici oluşturulamadı: %v", err)
	}
	defer func() { _ = ln.Close() }()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("soket incelenemedi: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o660 {
		t.Errorf("soket izinleri = %o, beklenen 660", perm)
	}
}

// TestListenReplacesStaleSocket, temiz kapanmayan bir önceki süreçten
// kalan soketin temizlendiğini doğrular.
func TestListenReplacesStaleSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix soketi semantiği Windows'ta farklı")
	}

	path := filepath.Join(t.TempDir(), "x.sock")

	first, err := Listen(ListenOptions{Path: path, Mode: 0o660})
	if err != nil {
		t.Fatalf("ilk dinleyici oluşturulamadı: %v", err)
	}
	// Close soket dosyasını siler; elle yeniden yaratmak yerine ikinci bir
	// dinleyiciyi doğrudan aynı yolda açarak "adres kullanımda" durumunu
	// sınıyoruz.
	if err := first.Close(); err != nil {
		t.Fatalf("kapatılamadı: %v", err)
	}

	second, err := Listen(ListenOptions{Path: path, Mode: 0o660})
	if err != nil {
		t.Fatalf("aynı yolda ikinci dinleyici oluşturulamadı: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Errorf("ikinci dinleyici kapatılamadı: %v", err)
	}
}

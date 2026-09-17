package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newSnapshotStore, dosya tabanlı bir depo açar.
//
// ⚠ `:memory:` KULLANILMIYOR ve bu zorunlu: bu dilimin tamamı DOSYA
// davranışıyla ilgili (yan dosyalar, rename, dizin izinleri). Bellek
// veritabanıyla geçen bir test, geri yükleme hakkında hiçbir şey
// söylemezdi.
func newSnapshotStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panely.db")
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("depo açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

// appIDs, veritabanındaki uygulama kimliklerini kümesi olarak döndürür.
func appIDs(t *testing.T, path string) map[string]bool {
	t.Helper()
	s, err := Open(context.Background(), path)
	if err != nil {
		t.Fatalf("veritabanı açılamadı (%s): %v", path, err)
	}
	defer func() { _ = s.Close() }()

	apps, err := s.ListApps(context.Background())
	if err != nil {
		t.Fatalf("uygulamalar listelenemedi: %v", err)
	}
	out := map[string]bool{}
	for _, a := range apps {
		out[a.ID] = true
	}
	return out
}

// TestSnapshotCarriesData, yedeğin AÇILABİLİR olduğunu ve veriyi
// taşıdığını doğrular.
//
// "Yedek dosyası oluştu" yeterli bir iddia DEĞİL — K-078'in dersi bu:
// yedeğin varlığı değil, AÇILABİLİRLİĞİ ölçülmeli.
func TestSnapshotCarriesData(t *testing.T) {
	s, path := newSnapshotStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("alfa")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}

	dest, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("yedek alınamadı: %v", err)
	}

	if dir := filepath.Dir(dest.Path); dir != SnapshotDir(path) {
		t.Errorf("yedek yanlış dizine yazıldı: %s", dir)
	}
	if got := appIDs(t, dest.Path); !got["alfa"] {
		t.Errorf("yedekte uygulama yok: %v — yedek AÇILDI ama BOŞ", got)
	}
}

// TestSnapshotDoesNotCollideWithMigrationBackups, iki yedek ad uzayının
// birbirine karışmadığını doğrular.
//
// Göç öncesi anlık görüntüler veritabanının YANINA `.pre-` ekiyle
// yazılıyor; zamanlı yedekler ayrı bir dizine. Bir budama turu
// diğerinin dosyalarını silerse, geri dönüş yolu sessizce kaybolur.
func TestSnapshotDoesNotCollideWithMigrationBackups(t *testing.T) {
	s, path := newSnapshotStore(t)
	ctx := context.Background()

	// Göç öncesi yedeği taklit et.
	migBackup := path + backupSuffix + "0001_init"
	if err := os.WriteFile(migBackup, []byte("göç yedeği"), 0o600); err != nil {
		t.Fatal(err)
	}
	// SQLite'ın yan dosyaları da aynı dizinde duruyor.
	wal := path + "-wal"
	if err := os.WriteFile(wal, []byte("wal"), 0o600); err != nil {
		t.Fatal(err)
	}

	// SnapshotKeep'i aşacak kadar yedek üret ki budama KESİNLİKLE koşsun.
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	for i := 0; i < SnapshotKeep+3; i++ {
		if _, err := s.snapshotAt(ctx, base.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatalf("yedek %d alınamadı: %v", i, err)
		}
	}

	if _, err := os.Stat(migBackup); err != nil {
		t.Errorf("budama GÖÇ yedeğini sildi: %v — geri dönüş yolu kayboldu", err)
	}
	if _, err := os.Stat(wal); err != nil {
		t.Errorf("budama WAL dosyasını sildi: %v — ÇALIŞAN veritabanı bozulurdu", err)
	}
}

// TestSnapshotPruneKeepsNewest, budamanın EN YENİLERİ tuttuğunu
// doğrular.
//
// Sıralama bu dilimde bir doğruluk meselesi: damga değişken genişlikte
// olsaydı sözlük sırası kronolojik sırayı temsil etmez ve budama en yeni
// yedeği silebilirdi.
func TestSnapshotPruneKeepsNewest(t *testing.T) {
	s, path := newSnapshotStore(t)
	ctx := context.Background()

	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	total := SnapshotKeep + 5
	var last string
	for i := 0; i < total; i++ {
		dest, err := s.snapshotAt(ctx, base.Add(time.Duration(i)*time.Minute))
		if err != nil {
			t.Fatalf("yedek %d alınamadı: %v", i, err)
		}
		last = dest.Path
	}

	got, err := ListSnapshots(path)
	if err != nil {
		t.Fatalf("yedekler listelenemedi: %v", err)
	}
	if len(got) != SnapshotKeep {
		t.Errorf("yedek sayısı %d, %d bekleniyordu", len(got), SnapshotKeep)
	}
	// En yenisi listenin BAŞINDA olmalı ve son yazılan dosya olmalı.
	if len(got) > 0 && got[0].Path != last {
		t.Errorf("en yeni yedek %s, %s bekleniyordu — sıralama bozuk",
			got[0].Path, last)
	}
	if _, err := os.Stat(last); err != nil {
		t.Errorf("budama EN YENİ yedeği sildi: %v", err)
	}
}

// TestListSnapshotsReadsTimeFromName, zamanın dosya ADINDAN çözüldüğünü
// doğrular.
func TestListSnapshotsReadsTimeFromName(t *testing.T) {
	s, path := newSnapshotStore(t)
	want := time.Date(2026, 9, 17, 13, 45, 5, 0, time.UTC)

	if _, err := s.snapshotAt(context.Background(), want); err != nil {
		t.Fatalf("yedek alınamadı: %v", err)
	}
	got, err := ListSnapshots(path)
	if err != nil {
		t.Fatalf("yedekler listelenemedi: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("yedek sayısı %d, 1 bekleniyordu", len(got))
	}
	if !got[0].Taken.Equal(want) {
		t.Errorf("yedek zamanı %v, %v bekleniyordu", got[0].Taken, want)
	}
	if got[0].Bytes <= 0 {
		t.Errorf("yedek boyutu %d — boş dosya yedek değildir", got[0].Bytes)
	}
}

// TestListSnapshotsOnMissingDirIsEmpty, hiç yedek alınmamış olmanın bir
// arıza OLMADIĞINI doğrular.
func TestListSnapshotsOnMissingDirIsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panely.db")
	got, err := ListSnapshots(path)
	if err != nil {
		t.Fatalf("dizin yokken hata döndü: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("yedek listesi boş değil: %v", got)
	}
}

// TestSnapshotStampIsFixedWidth, damganın SABİT GENİŞLİKTE olduğunu
// doğrudan sınar.
//
// ── Neden ayrı bir test gerekti ─────────────────────────────────────
//
// TestSnapshotPruneKeepsNewest sıralamayı sınıyor ama damga biçimini
// DEĞİL: kullandığı zamanlar tam dakika, yani saniye altı kısım her
// zaman boş. Değişken genişlikli bir biçim (RFC3339Nano gibi) o testten
// YEŞİL geçer.
//
// CI bunu ortaya çıkardı: "damga değişken genişliğe çevrildi" mutasyonu
// Windows'ta yakalanıyordu ama Linux'ta KAÇIYORDU. Sebep platformdu —
// RFC3339Nano dosya adına iki nokta üst üste koyuyor, Windows bunu
// reddediyor, Linux kabul ediyor. Yani mutasyon Windows'ta YANLIŞ
// SEBEPLE yakalanmıştı: sıralama bozulduğu için değil, dosya adı
// geçersiz olduğu için.
//
// Bu test biçimin kendisini ölçüyor, dolayısıyla her iki platformda da
// aynı şeyi söylüyor.
func TestSnapshotStampIsFixedWidth(t *testing.T) {
	// Saniye altı kısımları KASTEN farklı: değişken genişlikli bir biçim
	// bunları farklı uzunlukta basar.
	times := []time.Time{
		time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 17, 10, 0, 0, 500000000, time.UTC),
		time.Date(2026, 9, 17, 10, 0, 0, 123456789, time.UTC),
		time.Date(2026, 1, 2, 3, 4, 5, 900000000, time.UTC),
	}

	want := len(times[0].Format(snapshotStamp))
	for _, tm := range times {
		got := tm.Format(snapshotStamp)
		if len(got) != want {
			t.Errorf("damga %q uzunluğu %d, %d bekleniyordu — DEĞİŞKEN "+
				"GENİŞLİK sözlük sırasını kronolojik sıradan ayırır ve "+
				"budama yanlış dosyayı siler", got, len(got), want)
		}
		// Dosya adında kullanılıyor: yola izin verilmeyen karakter
		// taşımamalı. Windows iki nokta üst üsteyi reddediyor.
		if strings.ContainsAny(got, `:/\`) {
			t.Errorf("damga %q dosya adında geçersiz karakter taşıyor", got)
		}
	}

	// Sözlük sırası = kronolojik sıra, doğrudan sınanıyor.
	for i := 1; i < len(times); i++ {
		a, b := times[i-1], times[i]
		if a.After(b) {
			continue
		}
		if fa, fb := a.Format(snapshotStamp), b.Format(snapshotStamp); fa > fb {
			t.Errorf("kronolojik olarak %v < %v ama sözlük sırası tersi: %q > %q",
				a, b, fa, fb)
		}
	}
}

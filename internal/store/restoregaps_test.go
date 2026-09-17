package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// Bu dosyadaki testlerin tamamı MUTASYON TESTİNİN açtığı deliklerden
// doğdu. Her biri, restore_test.go'daki bir testin kanıtladığını
// sandığım ama kanıtlamadığı bir iddiayı sınıyor.
//
// Ayrı dosyada duruyorlar çünkü ortak bir sebepleri var ve o sebep
// kaydedilmeye değer: "yeşil bir test, korumak istediğin şeyi
// koruduğunu KANITLAMAZ."

// TestRestoreRemovesSidecarsWhenNoSafetyCopy, yan dosya silmenin
// GERÇEKTEN yük taşıdığı yolu sınar.
//
// ── Neden ayrı bir test gerekti ─────────────────────────────────────
//
// TestRestoreRemovesStaleWAL, silme tamamen kaldırıldığında bile YEŞİL
// kalıyordu. Sebep ölçüldü: `safetyCopy` veritabanını açıp kapatıyor ve
// SQLite temiz kapanışta yan dosyaları kendisi siliyor. Yani o test
// sonucun doğruluğunu kanıtlıyor, silmenin çalıştığını değil.
//
// Silme yalnızca güvenlik kopyası KOŞMADIĞINDA yük taşır: `.db` dosyası
// yokken yan dosyalar duruyorsa hiçbir şey checkpoint etmez. Bu gerçek
// bir kurtarma durumu — operatör bozuk veritabanını kenara alıp yedekten
// dönmek ister.
func TestRestoreRemovesSidecarsWhenNoSafetyCopy(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("alfa")); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// `.db` kenara alınıyor, yan dosyalar KASTEN bırakılıyor.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	for _, ext := range sqliteSidecars {
		if err := os.WriteFile(path+ext, []byte("yetim yan dosya"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// ── KONTROL GRUBU ────────────────────────────────────────────────
	// Yan dosyalar gerçekten orada mı? Değilse test hiçbir şey ölçmez.
	for _, ext := range sqliteSidecars {
		if _, err := os.Stat(path + ext); err != nil {
			t.Fatalf("kontrol grubu BAŞARISIZ: %s yok, test bir şey ölçmüyor",
				path+ext)
		}
	}

	safety, err := Restore(ctx, path, snap.Path)
	if err != nil {
		t.Fatalf("geri yükleme başarısız: %v", err)
	}
	if safety != "" {
		t.Errorf("veritabanı yokken güvenlik kopyası alındı: %s", safety)
	}

	// ASIL İDDİA: yetim yan dosyalar gitmiş olmalı. Burada onları
	// silebilecek BAŞKA hiçbir mekanizma yok.
	for _, ext := range sqliteSidecars {
		if _, err := os.Stat(path + ext); !os.IsNotExist(err) {
			t.Errorf("yetim yan dosya duruyor: %s — SQLite onu yeni "+
				"veritabanına oynatır", path+ext)
		}
	}
	if got := appIDs(t, path); !got["alfa"] {
		t.Errorf("geri yüklenen veritabanında alfa yok: %v", got)
	}
}

// TestRestoreRejectsEmptyMigrationTable, şeması VAR ama hiç göç kaydı
// OLMAYAN bir dosyanın reddedildiğini doğrular.
//
// TestRestoreRejectsForeignFile bunu kapsamıyor: oradaki dosyada
// `schema_migrations` tablosu hiç yok, yani sorgu HATA veriyor ve
// reddetme `err != nil` dalından geliyor. `n == 0` dalı hiç
// çalışmıyordu — mutasyon testi tam olarak bunu gösterdi.
func TestRestoreRejectsEmptyMigrationTable(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)
	if _, err := s.CreateApp(ctx, sampleApp("alfa")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Tablo VAR, içi BOŞ.
	empty := filepath.Join(t.TempDir(), "bos.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(empty))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version TEXT PRIMARY KEY, applied_at INTEGER NOT NULL) STRICT`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(ctx, path, empty); err == nil {
		t.Fatal("boş göç tablosu taşıyan dosya geri yüklendi — daemon " +
			"şemasız bir veritabanıyla açılırdı")
	}
	if got := appIDs(t, path); !got["alfa"] {
		t.Errorf("reddedilen geri yükleme veritabanını bozdu: %v", got)
	}
}

// TestRestoreRejectsStructurallyCorruptFile, integrity_check'in DÖNDÜĞÜ
// değerin gerçekten okunduğunu doğrular.
//
// TestRestoreRejectsTruncatedFile bunu kapsamıyor: kesilmiş dosyada
// sorgu HATA veriyor, yani reddetme yine `err != nil` dalından geliyor
// ve `result != "ok"` karşılaştırması hiç çalışmıyordu.
//
// Buradaki bozulma biçimi ÖLÇÜLEREK seçildi ve ilk seçim YANLIŞTI:
// dosyanın KUYRUĞUNU bozmak integrity_check'i hata döndürmeye zorluyor
// ("database disk image is malformed"), yani yine yanlış dal. İKİNCİ
// sayfayı sıfırlamak ise "ok" olmayan bir METİN döndürüyor
// ("btreeInitPage() returns error code 11") — aranan dal bu.
//
// Hatayı testin kendi kontrol grubu yakaladı, mutasyon betiği değil.
func TestRestoreRejectsStructurallyCorruptFile(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)

	// Birkaç uygulama: dosya tek sayfadan büyük olsun ki kuyrukta
	// bozulacak gerçek bir b-ağacı sayfası bulunsun.
	for _, id := range []string{"alfa", "beta", "gama", "delta", "epsilon"} {
		if _, err := s.CreateApp(ctx, sampleApp(id)); err != nil {
			t.Fatal(err)
		}
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	b, err := os.ReadFile(snap.Path)
	if err != nil {
		t.Fatal(err)
	}
	// İkinci sayfa (4096–8192) sıfırlanıyor. Ölçüldü: bu yedek 102400
	// bayt, yani 4096'lık 25 sayfa — ikinci sayfa her zaman var.
	const pageSize = 4096
	if len(b) < 2*pageSize {
		t.Fatalf("yedek beklenenden küçük (%d bayt)", len(b))
	}
	for i := pageSize; i < 2*pageSize; i++ {
		b[i] = 0
	}
	if err := os.WriteFile(snap.Path, b, 0o600); err != nil {
		t.Fatal(err)
	}

	// ── KONTROL GRUBU ────────────────────────────────────────────────
	// Bozulma gerçekten integrity_check'in METNİNDEN yakalanıyor mu,
	// yoksa dosya hiç mi açılamıyor? İkincisiyse bu test, kapsamak
	// istediği dalı yine ıskalar.
	probe, err := sql.Open("sqlite",
		"file:"+filepath.ToSlash(snap.Path)+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		t.Fatal(err)
	}
	var result string
	probeErr := probe.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result)
	_ = probe.Close()
	if probeErr != nil {
		t.Fatalf("kontrol grubu BAŞARISIZ: integrity_check hata verdi (%v) — "+
			"bu test `result != \"ok\"` dalını sınamıyor", probeErr)
	}
	if result == "ok" {
		t.Fatalf("kontrol grubu BAŞARISIZ: integrity_check %q dedi — "+
			"bozulma yakalanmıyor, test bir şey ölçmüyor", result)
	}

	if _, err := Restore(ctx, path, snap.Path); err == nil {
		t.Fatal("yapısı bozuk yedek kabul edildi")
	}
	if got := appIDs(t, path); !got["alfa"] {
		t.Errorf("reddedilen geri yükleme veritabanını bozdu: %v", got)
	}
}

// TestSnapshotDirIsSeparateSubdirectory, yedek dizininin veritabanının
// dizininden AYRI olduğunu doğrular.
//
// TestSnapshotCarriesData bunu kapsamıyordu: orada beklenti
// `SnapshotDir(path)` ile karşılaştırılıyor, yani sınanan fonksiyonun
// kendi çıktısıyla. SnapshotDir bozulsa iki taraf birlikte kayar ve
// test yeşil kalır — totolojik bir iddia.
//
// Burada beklenti ELLE yazılıyor.
func TestSnapshotDirIsSeparateSubdirectory(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "panely.db")

	got := SnapshotDir(dbPath)
	want := filepath.Join(dir, "backups")
	if got != want {
		t.Errorf("yedek dizini %q, %q bekleniyordu", got, want)
	}
	if got == filepath.Dir(dbPath) {
		t.Error("yedek dizini veritabanının dizinine EŞİT — budama " +
			"globu göç yedeklerini ve SQLite yan dosyalarını da görür")
	}
}

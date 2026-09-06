package store

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

// withMigrations, göç kaynağını test süresince değiştirir.
func withMigrations(t *testing.T, files fstest.MapFS) {
	t.Helper()
	prev := migrationFS
	migrationFS = files
	t.Cleanup(func() { migrationFS = prev })
}

// realMigrations, gömülü göç kümesini fstest'e kopyalar.
func realMigrations(t *testing.T) fstest.MapFS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(embeddedMigrations, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		b, err := embeddedMigrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		out["migrations/"+e.Name()] = &fstest.MapFile{Data: b}
	}
	return out
}

func backupsOf(t *testing.T, dbPath string) []string {
	t.Helper()
	m, err := filepath.Glob(dbPath + backupSuffix + "*")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestFreshDatabaseTakesNoBackup, yeni kurulumda boş bir kopya
// üretilmediğini doğrular.
func TestFreshDatabaseTakesNoBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panely.db")
	db, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	if got := backupsOf(t, path); len(got) != 0 {
		t.Errorf("taze veritabanında yedek alındı: %v — kaybedilecek veri yoktu", got)
	}
}

// TestBrokenMigrationIsRecoverableFromBackup, bu dilimin ASIL iddiasını
// sınar: bozuk bir göç daemon'u açılmaz yaparsa, veriye yedekten
// ulaşılabilir.
//
// ── Neden yedeğin VARLIĞINI doğrulamak yetmez ───────────────────────
//
// Bir dosyanın orada durması, açılabildiğini ve içinde veri olduğunu
// göstermez. WAL modunda ham kopyalama ile alınmış bir dosya var OLUR ama
// son yazmaları içermeyebilir. Bu yüzden test yedeği AÇIYOR ve içindeki
// satırı okuyor — geri dönüşün kendisi ölçülüyor, varlığı değil.
func TestBrokenMigrationIsRecoverableFromBackup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panely.db")

	// 1. Gerçek göçlerle aç ve GERÇEK veri yaz.
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	seedApp(t, db, "blog")
	// Orijinali OKU: geri yüklenen satır sabitlerle değil, gerçekten
	// yazılmış olanla karşılaştırılmalı. Sabitlerle karşılaştırmak,
	// yardımcının varsayılanları değişince testi yanlış yere baktırır.
	want, err := db.GetApp(ctx, "blog")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. Göç kümesine BOZUK bir dosya ekle ve yeniden aç.
	files := realMigrations(t)
	files["migrations/9999_bozuk.sql"] = &fstest.MapFile{
		Data: []byte("BU GECERLI SQL DEGIL;"),
	}
	withMigrations(t, files)

	if _, err := Open(ctx, path); err == nil {
		t.Fatal("bozuk göç hata döndürmedi — çökme döngüsü senaryosu kurulamadı")
	} else if !strings.Contains(err.Error(), "9999_bozuk.sql") {
		t.Errorf("hata hangi göçün battığını söylemiyor: %v", err)
	}

	// 3. Yedek alınmış ve hangi göçten önce alındığını söylüyor olmalı.
	got := backupsOf(t, path)
	if len(got) != 1 {
		t.Fatalf("yedek sayısı %d, 1 bekleniyordu: %v", len(got), got)
	}
	if !strings.HasSuffix(got[0], backupSuffix+"9999_bozuk") {
		t.Errorf("yedek adı %q — hangi göçten önce alındığını söylemeli", got[0])
	}

	// 4. ASIL İDDİA: yedek AÇILABİLİR ve veri İÇİNDE.
	//
	// Yedeği doğrudan açıyoruz; gerçek kurtarmada operatör de bunu yapar
	// (dosyayı yerine koyup daemon'u başlatır).
	withMigrations(t, realMigrations(t))
	restored, err := Open(ctx, got[0])
	if err != nil {
		t.Fatalf("yedek açılamadı — geri dönüş YOLU YOK: %v", err)
	}
	defer func() { _ = restored.Close() }()

	got2, err := restored.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("yedekte uygulama yok: %v", err)
	}
	if got2.ID != want.ID || got2.GitRepo != want.GitRepo ||
		got2.ContainerPort != want.ContainerPort || got2.Replicas != want.Replicas ||
		got2.Domain != want.Domain {
		t.Errorf("yedekteki satır orijinalden farklı: yedek=%+v orijinal=%+v",
			got2, want)
	}
}

// TestBackupIsNotOverwrittenOnRetry, ikinci denemenin ilk yedeği
// EZMEDİĞİNİ doğrular.
//
// systemd `Restart=on-failure` ile açılışı tekrar tekrar deniyor. Her
// deneme üzerine yazsaydı, ilk denemeden önceki temiz hâl kaybolur ve
// yedek "başarısız denemeden sonraki durum"a dönüşürdü.
//
// ── İddia neden İÇERİĞE bakıyor ─────────────────────────────────────
//
// İlk hâli yalnızca dosya SAYISINA ve boyutuna bakıyordu ve bu ayırt
// edici DEĞİLDİ: üzerine yazan bir uygulamada da tek, dolu bir dosya
// kalır. Mutasyon (Stat kontrolünü kaldır) yeşil geçti ve testin zayıf
// olduğunu gösterdi.
//
// Şimdi iki deneme ARASINA yeni bir satır yazılıyor. Yedek ezilirse o
// satır içine girer; ezilmezse girmez. Ayrım artık gözlenebilir.
func TestBackupIsNotOverwrittenOnRetry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "panely.db")

	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	seedApp(t, db, "blog")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	broken := realMigrations(t)
	broken["migrations/9999_bozuk.sql"] = &fstest.MapFile{Data: []byte("BOZUK;")}

	// 1. deneme: yedek burada alınmalı ve YALNIZCA "blog" içermeli.
	withMigrations(t, broken)
	if _, err := Open(ctx, path); err == nil {
		t.Fatal("1. deneme hata döndürmedi")
	}

	// Denemeler ARASINDA veritabanına yeni bir satır giriyor. Bu, ezilmeyi
	// gözlenebilir kılan işaretçi.
	withMigrations(t, realMigrations(t))
	db2, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	seedApp(t, db2, "ikinci")
	if err := db2.Close(); err != nil {
		t.Fatal(err)
	}

	// 2. ve 3. deneme: yedek DEĞİŞMEMELİ.
	withMigrations(t, broken)
	for i := 2; i <= 3; i++ {
		if _, err := Open(ctx, path); err == nil {
			t.Fatalf("%d. deneme hata döndürmedi", i)
		}
	}

	got := backupsOf(t, path)
	if len(got) != 1 {
		t.Fatalf("yedek sayısı %d — her deneme yeni dosya üretmemeli: %v", len(got), got)
	}

	withMigrations(t, realMigrations(t))
	snap, err := Open(ctx, got[0])
	if err != nil {
		t.Fatalf("yedek açılamadı: %v", err)
	}
	defer func() { _ = snap.Close() }()

	if _, err := snap.GetApp(ctx, "blog"); err != nil {
		t.Errorf("yedekte ilk satır yok: %v", err)
	}
	if _, err := snap.GetApp(ctx, "ikinci"); err == nil {
		t.Error("yedek denemeler arasında yazılan satırı içeriyor — " +
			"ÜZERİNE YAZILMIŞ; ilk denemeden önceki temiz hâl kayboldu")
	}
}

// TestBackupsArePruned, anlık görüntülerin sınırsız birikmediğini
// doğrular. Disk bu projenin ölçülmemiş kaynağı; yedek mekanizmasının
// kendisi onu doldurmamalı.
func TestBackupsArePruned(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "panely.db")

	// backupKeep + 2 tane sahte yedek üret.
	for i := 1; i <= backupKeep+2; i++ {
		name := path + backupSuffix + "000" + string(rune('0'+i)) + "_x"
		if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pruneBackups(path)

	got := backupsOf(t, path)
	if len(got) != backupKeep {
		t.Errorf("budama sonrası %d yedek kaldı, %d bekleniyordu: %v",
			len(got), backupKeep, got)
	}
	// En YENİLER kalmalı; en eskiler gitmeli.
	if strings.HasSuffix(got[0], "0001_x") {
		t.Error("en eski yedek korunmuş — budama yanlış uçtan siliyor")
	}
}

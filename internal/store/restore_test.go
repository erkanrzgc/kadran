package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree, veritabanı dosyasını ve SQLite'ın yan dosyalarını başka bir
// dizine kopyalar.
//
// Bu bir "çökme görüntüsü" üretiyor: süreç SIGKILL yerse ya da makine
// güç keserse diskte tam olarak bu üçlü kalır — checkpoint EDİLMEMİŞ bir
// WAL dahil. Geri yüklemenin gerçekte karşılaştığı durum budur, çünkü
// geri yükleme ihtiyacı çoğu zaman temiz bir kapanıştan DOĞMAZ.
func copyTree(t *testing.T, srcDB, dstDir string) string {
	t.Helper()
	dst := filepath.Join(dstDir, filepath.Base(srcDB))
	for _, ext := range append([]string{""}, sqliteSidecars...) {
		b, err := os.ReadFile(srcDB + ext)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatalf("%s okunamadı: %v", srcDB+ext, err)
		}
		if err := os.WriteFile(dst+ext, b, 0o600); err != nil {
			t.Fatalf("%s yazılamadı: %v", dst+ext, err)
		}
	}
	return dst
}

// TestRestoreRemovesStaleWAL, BU DİLİMİN ASIL İDDİASINI sınar.
//
// ── Neden bu test var ───────────────────────────────────────────────
//
// Geri yükleme yalnızca `.db` dosyasını değiştirip `-wal` ve `-shm`
// dosyalarını geride bırakırsa, SQLite açılışta ESKİ WAL'i YENİ dosyanın
// üzerine oynatır. Operatör "geri yükledim" der, panel eski veriyi
// gösterir ve arada hiçbir hata mesajı yoktur.
//
// Bu tuzak SIRADAN bir birim testinde GÖRÜNMEZ: taze bir geçici dizinde
// yan dosya yoktur, dolayısıyla silmeyi unutan bir uygulama da yeşil
// geçer. Test bu yüzden yan dosyaları KASTEN var ediyor.
//
// Kontrol grubu da içeride: çökme görüntüsü önce AYRI bir dizinde
// açılıyor ve "beta"nın gerçekten WAL'de durduğu doğrulanıyor. O adım
// olmasaydı test, yan dosyalar boş olduğu için de yeşil geçebilirdi —
// yani hiçbir şey kanıtlamazdı.
func TestRestoreRemovesStaleWAL(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)

	// 1. Yedeklenecek durum.
	if _, err := s.CreateApp(ctx, sampleApp("alfa")); err != nil {
		t.Fatalf("alfa yaratılamadı: %v", err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatalf("yedek alınamadı: %v", err)
	}

	// 2. Yedekten SONRA gelen yazma. Az sayıda satır otomatik
	//    checkpoint eşiğini (1000 sayfa) tetiklemez, yani bu kayıt
	//    `-wal` dosyasında durur — testin dayandığı gerçek bu.
	if _, err := s.CreateApp(ctx, sampleApp("beta")); err != nil {
		t.Fatalf("beta yaratılamadı: %v", err)
	}

	// 3. Çökme görüntüsü: dosyalar AÇIKKEN kopyalanıyor.
	crashDir := t.TempDir()
	crashed := copyTree(t, path, crashDir)

	// ── KONTROL GRUBU ────────────────────────────────────────────────
	// Çökme görüntüsü gerçekten "beta"yı taşıyor mu? Taşımıyorsa
	// aşağıdaki iddia boş bir iddia olurdu.
	if got := appIDs(t, crashed); !got["beta"] {
		t.Fatalf("kontrol grubu BAŞARISIZ: çökme görüntüsünde beta yok "+
			"(%v) — yan dosyalar boş, test hiçbir şey kanıtlamaz", got)
	}

	// 4. Depoyu kapat ve çökme görüntüsünü geri yerleştir: artık diskte
	//    bayat bir WAL taşıyan bir veritabanı var.
	if err := s.Close(); err != nil {
		t.Fatalf("depo kapatılamadı: %v", err)
	}
	for _, ext := range append([]string{""}, sqliteSidecars...) {
		b, err := os.ReadFile(crashed + ext)
		if err != nil {
			continue
		}
		if err := os.WriteFile(path+ext, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// 5. ASIL İŞLEM.
	if _, err := Restore(ctx, path, snap.Path); err != nil {
		t.Fatalf("geri yükleme başarısız: %v", err)
	}

	// 6. Yan dosyalar GİTMİŞ olmalı.
	for _, ext := range sqliteSidecars {
		if _, err := os.Stat(path + ext); !os.IsNotExist(err) {
			t.Errorf("bayat yan dosya duruyor: %s — SQLite onu yeni "+
				"veritabanına oynatır ve geri yükleme sessizce geri alınır",
				path+ext)
		}
	}

	// 7. ASIL İDDİA: veritabanı yedeğin hâlinde.
	got := appIDs(t, path)
	if !got["alfa"] {
		t.Errorf("geri yüklenen veritabanında alfa yok: %v", got)
	}
	if got["beta"] {
		t.Errorf("geri yükleme SONRASI beta hâlâ var: %v — bayat WAL "+
			"yeni veritabanına oynatıldı, geri yükleme HİÇ OLMADI", got)
	}
}

// TestRestoreTakesSafetyCopy, geri yüklemenin kendisinin geri
// alınabilir olduğunu doğrular.
//
// Geri yükleme yıkıcı bir işlem: yanlış yedeği seçen operatör, üzerine
// yazdığı durumu kaybeder. K-078'in dersi burada aynen geçerli — geri
// alınamaz bir adım, yedeksiz atılmaz.
func TestRestoreTakesSafetyCopy(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)

	if _, err := s.CreateApp(ctx, sampleApp("alfa")); err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Yedekten sonra gelen ve geri yüklemeyle KAYBOLACAK olan durum.
	if _, err := s.CreateApp(ctx, sampleApp("beta")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	safety, err := Restore(ctx, path, snap.Path)
	if err != nil {
		t.Fatalf("geri yükleme başarısız: %v", err)
	}
	if safety == "" {
		t.Fatal("güvenlik kopyası alınmadı — yanlış yedeği seçen " +
			"operatörün dönüş yolu YOK")
	}

	// Güvenlik kopyası, geri yüklemeden ÖNCEKİ hâli taşımalı.
	got := appIDs(t, safety)
	if !got["beta"] {
		t.Errorf("güvenlik kopyasında beta yok: %v — kopya WAL'deki "+
			"yazmaları kaçırdı, yani kaybolan veriyi kurtaramaz", got)
	}
}

// TestRestoreRejectsForeignFile, Panely veritabanı OLMAYAN bir dosyanın
// geri yüklenmediğini doğrular.
//
// Yabancı bir dosyayı yerine koymak daemon'u açılmaz yapardı ve o anda
// elimizdeki çalışan veritabanı çoktan gitmiş olurdu.
func TestRestoreRejectsForeignFile(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)
	if _, err := s.CreateApp(ctx, sampleApp("alfa")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	// Geçerli bir SQLite dosyası, ama Panely şeması YOK.
	foreign := filepath.Join(t.TempDir(), "yabanci.db")
	fdb, err := sql.Open("sqlite", "file:"+filepath.ToSlash(foreign))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fdb.ExecContext(ctx, `CREATE TABLE baska (x INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if err := fdb.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(ctx, path, foreign); err == nil {
		t.Fatal("yabancı dosya geri yüklendi — daemon açılmaz hâle gelirdi")
	}

	// Ve mevcut veritabanı DOKUNULMAMIŞ olmalı.
	if got := appIDs(t, path); !got["alfa"] {
		t.Errorf("reddedilen geri yükleme veritabanını bozdu: %v", got)
	}
}

// TestRestoreRejectsMissingFile, var olmayan yedeğin anlaşılır bir
// hatayla reddedildiğini doğrular.
func TestRestoreRejectsMissingFile(t *testing.T) {
	ctx := context.Background()
	s, path := newSnapshotStore(t)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	_, err := Restore(ctx, path, filepath.Join(t.TempDir(), "yok.db"))
	if err == nil {
		t.Fatal("var olmayan yedek kabul edildi")
	}
	if !strings.Contains(err.Error(), "bulunamadı") {
		t.Errorf("hata mesajı sebebi söylemiyor: %v", err)
	}
}

// TestRestoreRejectsTruncatedFile, YARIM bir yedeğin geri
// yüklenmediğini doğrular.
//
// Kesilme, yedek dosyasının en gerçekçi bozulma biçimi: disk dolar ya da
// süreç kopyalama sırasında öldürülür. Bunu geri yükledikten SONRA
// öğrenmek çok geç olurdu — o anda elimizdeki çalışan veritabanı gitmiş
// olur.
//
// ⚠ Testin İLK hâli dosyanın ortasındaki 1 KiB'ı XOR'luyordu ve
// KIRMIZI verdi: integrity_check "ok" döndü. Sebep ölçüldü — SQLite
// sayfa başına sağlama tutmuyor, o aralık da sayfanın boş alanıydı.
// Yani testin ilk hâli, kodun sahip OLMADIĞI bir güvenceyi sınıyordu.
// Doğrulayıcının yorumu buna göre düzeltildi; test ise gerçekten
// yakalanan bir bozulmayı sınıyor.
func TestRestoreRejectsTruncatedFile(t *testing.T) {
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

	// Kopyalama yarıda kalmış gibi kes. Başlık sağlam kalıyor, yani
	// dosya hâlâ "SQLite" görünüyor — reddin yapı denetiminden gelmesi
	// gerekiyor, dosya türü tanınmadığı için değil.
	b, err := os.ReadFile(snap.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 2048 {
		t.Fatalf("yedek beklenenden küçük (%d bayt)", len(b))
	}
	if err := os.WriteFile(snap.Path, b[:len(b)/2], 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Restore(ctx, path, snap.Path); err == nil {
		t.Fatal("yarım yedek kabul edildi")
	}
	if got := appIDs(t, path); !got["alfa"] {
		t.Errorf("reddedilen geri yükleme veritabanını bozdu: %v", got)
	}
}

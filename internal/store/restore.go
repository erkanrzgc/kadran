package store

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// restorePrefix, geri yükleme ÖNCESİ alınan güvenlik kopyalarının ad
// ekidir.
//
// Zamanlı yedeklerden (snapshotPrefix) ayrı: budama onları görmemeli.
// Gerekçe snapshot.go'daki pruneSnapshots yorumunda.
const restorePrefix = "pre-restore-"

// sqliteSidecars, SQLite'ın veritabanı dosyasının yanında tuttuğu yan
// dosyaların ekleridir.
//
// Geri yükleme `.db` dosyasını değiştirip bunları geride bırakırsa,
// SQLite açılışta ESKİ WAL'i YENİ dosyanın üzerine oynatmaya çalışır.
// Sonuç en iyi ihtimalle "geri yükleme hiç olmamış gibi" bir
// veritabanı, en kötüsünde bozulma.
//
// ── Bu silmenin NE ZAMAN yük taşıdığı ÖLÇÜLDÜ ───────────────────────
//
// Normal yolda taşımıyor. `safetyCopy` veritabanını açıp kapatıyor;
// SQLite temiz kapanışta WAL'i checkpoint edip her iki yan dosyayı da
// KENDİSİ siliyor. Ölçüldü: safetyCopy'den önce ikisi de var, sonra
// ikisi de yok.
//
// Bunu mutasyon testi ortaya çıkardı — silme tamamen kaldırıldığında
// TestRestoreRemovesStaleWAL YEŞİL kaldı. Yani o test, silmenin
// çalıştığını DEĞİL, sonucun doğru olduğunu kanıtlıyor; ikisi aynı şey
// değil (K-080: yeşil kalan mutasyonun üç sebebinden biri "gizli kod
// tutarsızlığı" idi, buradaki tam olarak o).
//
// Silme, güvenlik kopyası KOŞMADIĞINDA yük taşıyor: `.db` yokken yan
// dosyalar duruyorsa (operatör bozuk dosyayı kenara aldı) kimse
// checkpoint etmez ve yetim WAL yeni veritabanına oynatılırdı.
// TestRestoreRemovesSidecarsWhenNoSafetyCopy tam o yolu sınıyor.
var sqliteSidecars = []string{"-wal", "-shm"}

// Restore, bir yedeği veritabanının yerine koyar ve aldığı güvenlik
// kopyasının yolunu döndürür.
//
// ── Daemon KAPALI olmalı ────────────────────────────────────────────
//
// Çalışan bir panelyd'nin altından veritabanını çekmek, açık dosya
// tanıtıcısı eski inode'u tutmaya devam ettiği için sessiz ve
// açıklanamaz bir duruma yol açar. Çağıran taraf (cmd/panelyd) soketi
// yoklayarak daemon'ın kapalı olduğunu doğruluyor; burada bu bir ÖN
// KOŞULdur.
//
// ── Sıra neden BU sıra ──────────────────────────────────────────────
//
//  1. Yedek DOĞRULANIR. Bozuk bir dosyayı yerine koymak, elimizdeki
//     çalışan veritabanını bozuk bir dosyayla değiştirmek olurdu.
//  2. Mevcut veritabanının GÜVENLİK KOPYASI alınır. Geri yükleme yıkıcı
//     bir işlem ve K-078'in dersi aynen geçerli: geri alınamaz bir adım,
//     yedeksiz atılamaz. Yanlış yedeği seçen operatörün dönüş yolu bu.
//  3. Yan dosyalar SİLİNİR — güvenlik kopyası zaten WAL'deki yazmaları
//     içerdiği için burada kaybedilen bir şey yok.
//  4. Yedek yerine KONUR (geçici dosya + rename, yani yarım kalmış bir
//     kopya asla `panely.db` adını almaz).
func Restore(ctx context.Context, dbPath, snapshotPath string) (string, error) {
	if dbPath == ":memory:" || dbPath == "" {
		return "", fmt.Errorf("bellek veritabanına geri yükleme yapılamaz")
	}

	// 1. Yedek gerçekten açılabilir ve Panely şeması taşıyor mu?
	if err := validateSnapshot(ctx, snapshotPath); err != nil {
		return "", err
	}

	// 2. Güvenlik kopyası. Veritabanı yoksa (ilk kurulum, ya da elle
	//    silinmiş) kopyalanacak bir şey de yok — bu bir hata değil.
	safety := ""
	if _, err := os.Stat(dbPath); err == nil {
		safety, err = safetyCopy(ctx, dbPath)
		if err != nil {
			return "", err
		}
	}

	// 3. Yan dosyalar. Sıra önemli: güvenlik kopyası ALINDIKTAN sonra.
	for _, ext := range sqliteSidecars {
		if err := os.Remove(dbPath + ext); err != nil && !os.IsNotExist(err) {
			return "", fmt.Errorf(
				"eski yan dosya silinemedi (%s) — geri yükleme YARIM "+
					"kalırdı ve SQLite eski WAL'i yeni dosyaya oynatırdı: %w",
				dbPath+ext, err)
		}
	}

	// 4. Yerine koy.
	if err := copyFileAtomic(snapshotPath, dbPath); err != nil {
		return "", err
	}
	return safety, nil
}

// validateSnapshot, dosyanın açılabilir bir SQLite veritabanı olduğunu ve
// Panely şemasını taşıdığını doğrular.
//
// ⚠ Store.Open KULLANILMIYOR ve bu kasıtlı: Open göçleri UYGULAR, yani
// doğrulamak istediğimiz dosyayı DEĞİŞTİRİRDİ. Salt okunur açıyoruz.
func validateSnapshot(ctx context.Context, path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("yedek bulunamadı (%s): %w", path, err)
	}

	// mode=ro: göç de yazmaz, yanlışlıkla yan dosya da oluşmaz.
	db, err := sql.Open("sqlite",
		"file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return fmt.Errorf("yedek açılamadı (%s): %w", path, err)
	}
	defer func() { _ = db.Close() }()

	// integrity_check, b-ağacı yapısını dolaşır: kesilmiş dosya, bozulmuş
	// sayfa başlığı ve sayfa sonunu taşan hücreler burada yakalanır.
	// Yedeğin bozukluğunu geri yükledikten SONRA öğrenmek çok geç olurdu.
	//
	// ⚠ NE YAKALAMADIĞI ÖLÇÜLDÜ. SQLite sayfa başına sağlama (checksum)
	// TUTMAZ; bir sayfanın BOŞ ALANINDAKİ bit çevrilmeleri yapıyı
	// bozmadığı için integrity_check "ok" döner. Ölçüm: 28 KiB'lık bir
	// veritabanında 1024–2048 aralığı XOR'landı → "ok"; aynı dosyada
	// kuyruk bozulması, ortası sıfırlanmış sayfa, bozuk başlık ve
	// kesilme → HEPSİ yakalandı.
	//
	// Yani buradaki güvence "yedek bit düzeyinde sağlam"a DEĞİL,
	// "yedek açılabilir ve yapısı tutarlı"ya eşittir. Gerçek bit çürümesi
	// güvencesi ayrı bir sağlama dosyası ister ve bu dilimin kapsamında
	// DEĞİL — kapsamda sanılması, olmayan bir korumaya güvenmek olurdu.
	var result string
	if err := db.QueryRowContext(ctx,
		`PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("yedek okunamadı (%s): %w", path, err)
	}
	if result != "ok" {
		return fmt.Errorf("yedek BOZUK (%s): integrity_check = %q",
			path, result)
	}

	// Şema kontrolü: rastgele bir SQLite dosyası Panely veritabanı
	// değildir. Bunu geri yüklemek daemon'u açılmaz yapardı.
	var n int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		return fmt.Errorf(
			"yedek Panely veritabanı değil (%s): schema_migrations "+
				"okunamadı: %w", path, err)
	}
	if n == 0 {
		return fmt.Errorf(
			"yedekte hiç göç kaydı yok (%s) — boş ya da yabancı bir dosya",
			path)
	}
	return nil
}

// safetyCopy, mevcut veritabanının tutarlı bir kopyasını yedek dizinine
// alır.
//
// `VACUUM INTO` kullanıyor, ham kopya değil: WAL'de bekleyen yazmalar da
// kopyaya girmek ZORUNDA, çünkü bir sonraki adım tam da o WAL dosyasını
// siliyor.
func safetyCopy(ctx context.Context, dbPath string) (string, error) {
	dir := SnapshotDir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("yedek dizini oluşturulamadı (%s): %w", dir, err)
	}

	dest := filepath.Join(dir,
		restorePrefix+time.Now().UTC().Format(snapshotStamp)+snapshotExt)
	if _, err := os.Stat(dest); err == nil {
		// Aynı saniye içinde ikinci deneme: var olan kopya zaten bu anın
		// görüntüsü.
		return dest, nil
	}

	db, err := sql.Open("sqlite", buildDSN(dbPath))
	if err != nil {
		return "", fmt.Errorf("veritabanı açılamadı (%s): %w", dbPath, err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "VACUUM INTO ?",
		filepath.ToSlash(dest)); err != nil {
		return "", fmt.Errorf(
			"geri yükleme öncesi güvenlik kopyası alınamadı (%s) — "+
				"geri dönüş yolu olmadan devam EDİLMEZ: %w", dest, err)
	}
	return dest, nil
}

// copyFileAtomic, kaynağı hedefe geçici dosya üzerinden kopyalar.
//
// Doğrudan hedefe yazmak, kopyalama yarıda kalırsa (disk dolu, süreç
// öldürüldü) `panely.db` adını taşıyan BOZUK bir dosya bırakırdı.
// Geçici dosya + rename ile hedef ya eski ya yeni hâldedir, arası yok.
func copyFileAtomic(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("yedek okunamadı (%s): %w", src, err)
	}
	defer func() { _ = in.Close() }()

	// Geçici dosya HEDEFLE AYNI DİZİNDE: rename yalnızca aynı dosya
	// sistemi içinde atomiktir.
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".restore-*")
	if err != nil {
		return fmt.Errorf("geçici dosya oluşturulamadı: %w", err)
	}
	tmpName := tmp.Name()
	// Başarısız her yolda geçici dosya temizlenir; başarıda rename onu
	// zaten ortadan kaldırmış olur ve bu Remove sessizce başarısız olur.
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()

	// 0600: veritabanı ortam değişkenlerini ve denetim zincirini taşıyor.
	// CreateTemp zaten 0600 veriyor, ama açıkça yazmak niyeti belgeliyor
	// ve ileride bir umask değişikliği bunu sessizce gevşetemez.
	if err := tmp.Chmod(0o600); err != nil {
		return fmt.Errorf("geçici dosya izinleri ayarlanamadı: %w", err)
	}
	if _, err := io.Copy(tmp, in); err != nil {
		return fmt.Errorf("yedek kopyalanamadı: %w", err)
	}
	// fsync: rename'den önce veri gerçekten diskte olmalı. Aksi hâlde bir
	// güç kesintisi, adı doğru ama içi boş bir dosya bırakabilir.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("yedek diske yazılamadı: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("geçici dosya kapatılamadı: %w", err)
	}

	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("yedek yerine konulamadı (%s): %w", dst, err)
	}
	return nil
}

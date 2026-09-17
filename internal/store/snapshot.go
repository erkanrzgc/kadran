package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// snapshotDirName, zamanlı yedeklerin tutulduğu alt dizindir.
//
// ── Neden veritabanının YANINA değil, ayrı bir dizine? ───────────────
//
// Göç öncesi anlık görüntüler (backup.go) dosyanın yanına, `.pre-` ekiyle
// yazılıyor. Zamanlı yedekler aynı yere yazılsaydı iki ad uzayı iç içe
// geçerdi ve budama globları birbirinin dosyalarını görürdü. Bu kuramsal
// bir kaygı değil: aynı dizinde SQLite'ın kendi yan dosyaları da
// (`panely.db-wal`, `panely.db-shm`) duruyor ve `panely.db*` gibi geniş
// bir glob onları da yakalar — yani bir budama turu ÇALIŞAN veritabanının
// WAL'ini silebilirdi.
//
// Ayrı dizin bu sınıfı tamamen kapatıyor: budama yalnızca kendi
// dizinindeki kendi desenine bakıyor, ve dizinin tamamı ileride tek bir
// senkronizasyon hedefi (R2, rsync) olarak gösterilebiliyor.
const snapshotDirName = "backups"

// snapshotPrefix ve snapshotExt, zamanlı yedeklerin ad şablonudur.
//
// Geri yükleme öncesi güvenlik kopyaları KASTEN başka bir ön ek taşıyor
// (restorePrefix): budama onları görmemeli.
const (
	snapshotPrefix = "panely-"
	snapshotExt    = ".db"
)

// snapshotStamp, yedek adlarındaki zaman damgasının biçimidir.
//
// ⚠ SABİT GENİŞLİK ŞART. Bu proje aynı tuzağa bir kez düştü: RFC3339Nano
// sondaki sıfırları kırpar, yani damga DEĞİŞKEN genişlikte olur ve
// sözlük sırası kronolojik sırayı TEMSİL ETMEZ. Budama en eskiyi silmek
// için sıralamaya güveniyor; yanlış sıralama, en YENİ yedeği silerdi.
//
// UTC de şart: sunucu yerel saati yaz saatine geçerse bir saat geri
// giden damgalar üretilir ve sıra yine bozulur.
const snapshotStamp = "20060102T150405Z"

// SnapshotKeep, saklanacak zamanlı yedek sayısıdır.
//
// Göç öncesi yedeklerdeki backupKeep'ten (3) ayrı tutuluyor çünkü soru
// farklı: orada "son üç göç turu", burada "kaç tur geriye dönebilmek
// istiyoruz". Yedekler saatlik alınıyorsa 24 tane yaklaşık bir günlük
// pencere demek.
const SnapshotKeep = 24

// Snapshot, veritabanının tutarlı bir kopyasını yedek dizinine yazar ve
// eskilerini budar. Yazılan dosyanın tam yolunu döndürür.
//
// ── Neden `cp` değil `VACUUM INTO` ──────────────────────────────────
//
// Veritabanı WAL modunda. Ham dosya kopyası en son yazmaları
// İÇERMEYEBİLİR: onlar henüz `-wal` dosyasında duruyor olabilir ve
// kopyalanan `.db` sessizce eski bir duruma döner. `VACUUM INTO` motorun
// kendi anlık görüntüsünü alır — tek, tutarlı, açılmaya hazır bir dosya.
//
// Bu, göç öncesi yedekle aynı gerekçe (backup.go) ve kasten aynı
// mekanizma: iki farklı yoldan yedek almak, ikisinden birinin sessizce
// bozulması demekti.
func (s *Store) Snapshot(ctx context.Context) (SnapshotInfo, error) {
	return s.snapshotAt(ctx, time.Now().UTC())
}

// snapshotAt, saati DIŞARIDAN alır.
//
// Testler zamanı kontrol edebilmeli: budamanın doğru dosyayı sildiğini
// sınamak için birden fazla damga üretmek gerekiyor ve gerçek saatle
// beklemek testi ya yavaş ya da kırılgan yapardı.
func (s *Store) snapshotAt(ctx context.Context, now time.Time) (SnapshotInfo, error) {
	// Bellek veritabanının dosyası yok; yedeklenecek bir şey de yok.
	if s.path == ":memory:" || s.path == "" {
		return SnapshotInfo{}, fmt.Errorf("bellek veritabanı yedeklenemez")
	}

	dir := SnapshotDir(s.path)
	// 0700: yedekler veritabanının TAMAMINI taşıyor — ortam değişkenleri
	// ve denetim kayıtları dahil. Veritabanı dosyasının kendisinden daha
	// açık olmaları için hiçbir sebep yok.
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return SnapshotInfo{}, fmt.Errorf(
			"yedek dizini oluşturulamadı (%s): %w", dir, err)
	}

	stamp := now.UTC().Truncate(time.Second)
	dest := filepath.Join(dir,
		snapshotPrefix+stamp.Format(snapshotStamp)+snapshotExt)

	// VACUUM INTO hedefin var OLMAMASINI ister. Aynı saniye içinde iki
	// yedek istenirse (elle tetikleme + zamanlayıcı) ad çakışır; bunu
	// hata saymak yerine var olanı geçerli kabul ediyoruz — o dosya zaten
	// bu saniyenin tutarlı görüntüsü.
	if _, err := os.Stat(dest); err != nil {
		// Yol BAĞLI PARAMETRE olarak geçiyor, dizgeye gömülmüyor:
		// enjeksiyon sınıfını tamamen kapatıyor (gerekçe backup.go'da
		// ölçüldü).
		if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?",
			filepath.ToSlash(dest)); err != nil {
			return SnapshotInfo{}, fmt.Errorf(
				"yedek alınamadı (%s): %w", dest, err)
		}
	}

	info := SnapshotInfo{Path: dest, Taken: stamp}
	if fi, err := os.Stat(dest); err == nil {
		info.Bytes = fi.Size()
	}

	pruneSnapshots(dir)
	return info, nil
}

// SnapshotDir, verilen veritabanı yolu için yedek dizinini döndürür.
//
// Dışa açık: geri yükleme yolu ve daemon aynı dizini hesaplamak zorunda
// ve iki yerde ayrı ayrı birleştirmek, birinin sessizce kaymasına
// davetiye olurdu.
func SnapshotDir(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), snapshotDirName)
}

// pruneSnapshots, en yeni SnapshotKeep tanesi dışındaki yedekleri siler.
//
// Hata döndürmüyor: budama başarısız olsa bile yedek ALINDI. Burada
// başarısızlığı ölümcül saymak, disk dolduğunda yedeklemeyi tamamen
// durdururdu — üstelik elimizde taze bir yedek varken. Göç öncesi
// budamayla (backup.go) aynı gerekçe.
//
// ⚠ Yalnızca snapshotPrefix desenine bakıyor. Geri yükleme öncesi
// güvenlik kopyaları (restorePrefix) KASTEN kapsam dışı: onlar operatör
// tetiklemeli, nadir ve geri dönüşün son halkası.
func pruneSnapshots(dir string) {
	matches, err := filepath.Glob(
		filepath.Join(dir, snapshotPrefix+"*"+snapshotExt))
	if err != nil || len(matches) <= SnapshotKeep {
		return
	}
	// Damga sabit genişlikte ve UTC (snapshotStamp), dolayısıyla sözlük
	// sırası = kronolojik sıra.
	sort.Strings(matches)
	for _, old := range matches[:len(matches)-SnapshotKeep] {
		_ = os.Remove(old)
	}
}

// ListSnapshots, bu deponun yedeklerini en YENİDEN eskiye döndürür.
//
// Paket düzeyindeki ikizini sarıyor: daemon yolu ayrıca taşımak zorunda
// kalmasın. Geri yükleme yolu depoyu AÇMADAN çalıştığı için paket
// düzeyindeki biçim de gerekli.
func (s *Store) ListSnapshots() ([]SnapshotInfo, error) {
	return ListSnapshots(s.path)
}

// ListSnapshots, yedek dizinindeki zamanlı yedekleri en YENİDEN eskiye
// doğru sıralı döndürür.
//
// Dizin yoksa boş liste ve nil hata döner: henüz hiç yedek alınmamış
// olmak bir arıza değil.
func ListSnapshots(dbPath string) ([]SnapshotInfo, error) {
	dir := SnapshotDir(dbPath)
	matches, err := filepath.Glob(
		filepath.Join(dir, snapshotPrefix+"*"+snapshotExt))
	if err != nil {
		return nil, fmt.Errorf("yedekler listelenemedi (%s): %w", dir, err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))

	out := make([]SnapshotInfo, 0, len(matches))
	for _, m := range matches {
		fi, err := os.Stat(m)
		if err != nil {
			// Budama tam bu anda silmiş olabilir; listeyi tamamen
			// başarısız saymak yerine o satırı atlıyoruz.
			continue
		}
		out = append(out, SnapshotInfo{
			Path:  m,
			Bytes: fi.Size(),
			Taken: snapshotTime(m),
		})
	}
	return out, nil
}

// SnapshotInfo, tek bir yedeği tanımlar.
type SnapshotInfo struct {
	Path  string
	Bytes int64
	// Taken, dosya ADINDAN çözülür, değiştirilme zamanından DEĞİL.
	// Dosya kopyalanırsa mtime kayar; ad, yedeğin gerçekten hangi ana
	// ait olduğunu taşıyan tek alandır. Çözülemezse sıfır değer döner.
	Taken time.Time
}

// snapshotTime, dosya adındaki damgayı çözer.
func snapshotTime(path string) time.Time {
	base := filepath.Base(path)
	base = strings.TrimPrefix(base, snapshotPrefix)
	base = strings.TrimSuffix(base, snapshotExt)
	t, err := time.Parse(snapshotStamp, base)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

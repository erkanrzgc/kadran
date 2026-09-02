package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// ErrAppIsLive, uygulamanın canlı bir sürümü olduğu için silinemediğini
// bildirir.
//
// Bu, Faz 1'de silmenin KASITLI sınırıdır. Canlı bir uygulamayı silmek
// yıkıcı bir işlemdir ve şartname §1.3 o sınıfı TOTP kapısına koyuyor.
// Kapı Faz 2'de gelince buradaki kısıt GEVŞETİLECEK — kısıtlanmayacak —
// ve gevşetme geriye dönük uyumludur. Gerekçenin tamamı api.proto'da.
var ErrAppIsLive = errors.New("uygulamanın canlı sürümü var — önce trafiği kaldırın")

// DeletedCounts, silmenin NEYİ yok ettiğini sayar.
type DeletedCounts struct {
	Releases    uint32
	Deployments uint32
}

// DeleteApp, uygulamayı ve kontrol düzlemindeki bütün kayıtlarını siler.
//
// ── Sıra veritabanı tarafından ZORLANIYOR ───────────────────────────
//
//	deployments → releases → apps
//
// `deployments` hem `apps(id)`'ye hem `releases(app_id, id)`'ye bakıyor;
// `releases` de `apps(id)`'ye. Ters sırada silme `FOREIGN KEY constraint
// failed` ile düşer. Bu ölçüldü: gerçek veritabanının bir kopyasında
// önce `releases` silinmeye çalışıldı ve reddedildi.
//
// ⚠ İlk ölçüm YETERSİZDİ ve bunu göstermedi: sıfır dağıtımı olan bir
// uygulama (`blog`) seçilmişti, yani bileşik yabancı anahtar hiç
// çalışmadı. Dağıtımı olan bir uygulamayla tekrarlanınca ortaya çıktı.
//
// ── Canlılık kontrolü İŞLEMİN İÇİNDE ────────────────────────────────
//
// Çağıran tarafta kontrol etmek yetmezdi: kontrol ile silme arasında bir
// dağıtım tamamlanabilir ve canlı bir uygulamanın kaydını silerdik —
// konteynerleri çalışmaya devam eder ama onlara ulaşacak hiçbir kayıt
// kalmazdı. Değişmez, onu koruyabilecek TEK yerde duruyor.
func (s *Store) DeleteApp(ctx context.Context, appID string) (DeletedCounts, error) {
	var out DeletedCounts

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("silme işlemi açılamadı: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM apps WHERE id = ?`, appID).Scan(&exists); err != nil {
		return out, fmt.Errorf("uygulama okunamadı: %w", err)
	}
	if exists == 0 {
		return out, ErrAppNotFound
	}

	var live string
	err = tx.QueryRowContext(ctx,
		`SELECT release_id FROM deployments
		 WHERE app_id = ? AND deactivated_at IS NULL`, appID).Scan(&live)
	switch {
	case err == nil:
		// Hangi sürümün canlı olduğunu SÖYLÜYORUZ: "silinemez" tek
		// başına operatöre ne yapacağını göstermiyor.
		return out, fmt.Errorf("%w (canlı sürüm: %s)", ErrAppIsLive, live)
	case !errors.Is(err, sql.ErrNoRows):
		return out, fmt.Errorf("canlı sürüm sorgulanamadı: %w", err)
	}

	deployments, err := deleteRows(ctx, tx,
		`DELETE FROM deployments WHERE app_id = ?`, appID)
	if err != nil {
		return out, fmt.Errorf("dağıtım geçmişi silinemedi: %w", err)
	}
	releases, err := deleteRows(ctx, tx, `DELETE FROM releases WHERE app_id = ?`, appID)
	if err != nil {
		return out, fmt.Errorf("sürümler silinemedi: %w", err)
	}
	if _, err := deleteRows(ctx, tx, `DELETE FROM apps WHERE id = ?`, appID); err != nil {
		return out, fmt.Errorf("uygulama silinemedi: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return out, fmt.Errorf("silme işlemi tamamlanamadı: %w", err)
	}
	out.Deployments, out.Releases = deployments, releases
	return out, nil
}

// deleteRows, silmeyi çalıştırır ve etkilenen satır sayısını döndürür.
func deleteRows(ctx context.Context, tx *sql.Tx, query, appID string) (uint32, error) {
	res, err := tx.ExecContext(ctx, query, appID)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return uint32(n), nil //nolint:gosec // satır sayısı negatif olamaz
}

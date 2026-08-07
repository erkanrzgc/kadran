package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// ReleaseStatus, bir sürümün yaşam döngüsündeki yeridir.
//
// Değerler panelyv1.ReleaseStatus ve göç 0002'deki CHECK ile AYNI olmak
// zorundadır; üçü de aynı sayıları kullanır.
type ReleaseStatus int

const (
	// ReleaseBuilding: sürüm kaydedildi, derleme sürüyor.
	ReleaseBuilding ReleaseStatus = 1
	// ReleaseBuilt: imaj üretildi ve kimliği kaydedildi.
	ReleaseBuilt ReleaseStatus = 2
	// ReleaseFailed: derleme başarısız.
	ReleaseFailed ReleaseStatus = 3
)

// ErrReleaseNotFound, istenen sürümün bulunmadığını bildirir.
var ErrReleaseNotFound = errors.New("sürüm bulunamadı")

// Release, bir commit'in tek bir derleme denemesidir.
type Release struct {
	AppID string
	ID    string
	Seq   uint32

	CommitSHA string
	Status    ReleaseStatus
	ImageID   string

	StartedAt  time.Time
	FinishedAt time.Time
	Detail     string
}

// StartRelease, yeni bir sürüm kaydı açar ve BUILDING durumunda döner.
//
// # Neden tek transaction?
//
// Sürüm adı `apps.release_seq`'ten türetiliyor. Sayacı okuyup ayrı bir
// deyimle yazmak, iki eşzamanlı dağıtımın aynı adı üretmesine açık
// olurdu — ve o ad host tarafında konteyner etiketidir, yani iki farklı
// sürüm aynı konteynerleri adresler. Sayaç artışı ile ekleme aynı
// transaction'da; ayrıca DSN `_txlock=immediate` kullanıyor, yani yazma
// kilidi en baştan alınıyor.
//
// Derleme BAŞLAMADAN yazılır: istemci düşse bile sürüm veritabanında
// adreslenebilir kalmalı.
func (s *Store) StartRelease(ctx context.Context, appID, commitSHA string) (Release, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Release{}, fmt.Errorf("sürüm transaction'ı açılamadı: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var seq uint32
	err = tx.QueryRowContext(ctx,
		`UPDATE apps SET release_seq = release_seq + 1, updated_at = ?
		 WHERE id = ? RETURNING release_seq`,
		time.Now().UnixNano(), appID,
	).Scan(&seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Release{}, fmt.Errorf("%w: %s", ErrAppNotFound, appID)
	case err != nil:
		return Release{}, fmt.Errorf("sürüm sayacı artırılamadı: %w", err)
	}

	rel := Release{
		AppID:     appID,
		ID:        ReleaseID(seq),
		Seq:       seq,
		CommitSHA: commitSHA,
		Status:    ReleaseBuilding,
		StartedAt: time.Now(),
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO releases (app_id, id, seq, commit_sha, status, started_at)
		 VALUES (?,?,?,?,?,?)`,
		rel.AppID, rel.ID, rel.Seq, rel.CommitSHA, int(rel.Status),
		rel.StartedAt.UnixNano(),
	); err != nil {
		return Release{}, fmt.Errorf("sürüm yazılamadı: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return Release{}, fmt.Errorf("sürüm kaydedilemedi: %w", err)
	}
	return rel, nil
}

// ReleaseID, sıra numarasından sürüm kimliğini üretir.
//
// Sonuç executor'ın `^[a-z0-9]{1,64}$` kısıtına uyar. Kimliğin bir kez
// üretilip SAKLANMASI kasıtlı: host tarafındaki konteyner etiketi bu
// değerdir, dolayısıyla şema değişse bile eski satırlar gerçek
// konteynerleri göstermeye devam etmelidir.
func ReleaseID(seq uint32) string { return "r" + strconv.FormatUint(uint64(seq), 10) }

// FinishRelease, sürümü BUILT olarak mühürler.
//
// imageID boşsa çağrı REDDEDİLİR. Docker'ın klasik derleyicisi derleme
// ortasında ölen bir yapı için de HTTP 200 döner; başarının tek pozitif
// kanıtı `aux` karesinden gelen imaj kimliğidir (K-042). Şemada da aynı
// kısıt var — buradaki kontrol, hatayı SQL hatası yerine anlaşılır bir
// mesajla döndürmek içindir, tek savunma değildir.
func (s *Store) FinishRelease(ctx context.Context, appID, releaseID, imageID string) error {
	if imageID == "" {
		return errors.New("sürüm imaj kimliği olmadan BUILT işaretlenemez — " +
			"derleme başarısının tek pozitif kanıtı aux karesidir")
	}
	return s.sealRelease(ctx, appID, releaseID, ReleaseBuilt, imageID, "")
}

// FailRelease, sürümü FAILED olarak mühürler.
//
// ⚠ Host tarafında imaj oluşmuş OLABİLİR: derleme akışı yarıda koparsa
// (istemci düşmesi) Docker derlemeyi tamamlamış olabilir ama aux karesi
// bize ulaşmamıştır. Yön güvenlidir — denetlemediğimiz bir şeyi başarılı
// saymıyoruz — ama Faz 2'nin öksüz imaj temizliği bunu bilmelidir.
func (s *Store) FailRelease(ctx context.Context, appID, releaseID, detail string) error {
	return s.sealRelease(ctx, appID, releaseID, ReleaseFailed, "", detail)
}

func (s *Store) sealRelease(
	ctx context.Context, appID, releaseID string,
	status ReleaseStatus, imageID, detail string,
) error {
	// `status = 1` koşulu bir mühürleme kaydını İKİNCİ kez mühürlemeyi
	// engeller: yalnızca BUILDING durumundaki bir satır geçiş yapabilir.
	// Aksi hâlde geç gelen bir hata, başarılı bir sürümü FAILED'a
	// çevirebilirdi.
	res, err := s.db.ExecContext(ctx,
		`UPDATE releases SET status = ?, image_id = ?, detail = ?, finished_at = ?
		 WHERE app_id = ? AND id = ? AND status = ?`,
		int(status), imageID, detail, time.Now().UnixNano(),
		appID, releaseID, int(ReleaseBuilding),
	)
	if err != nil {
		return fmt.Errorf("sürüm mühürlenemedi: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sürüm mühürleme sonucu okunamadı: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %s/%s (zaten mühürlenmiş olabilir)",
			ErrReleaseNotFound, appID, releaseID)
	}
	return nil
}

// ListReleases, uygulamanın sürümlerini en yeniden eskiye döner.
func (s *Store) ListReleases(ctx context.Context, appID string, limit int) ([]Release, error) {
	if limit <= 0 {
		limit = defaultReleaseLimit
	}
	if limit > maxReleaseLimit {
		limit = maxReleaseLimit
	}

	rows, err := s.db.QueryContext(ctx,
		releaseSelect+` WHERE app_id = ? ORDER BY seq DESC LIMIT ?`, appID, limit)
	if err != nil {
		return nil, fmt.Errorf("sürümler okunamadı: %w", err)
	}
	defer func() { _ = rows.Close() }()

	releases := []Release{}
	for rows.Next() {
		rel, err := scanRelease(rows)
		if err != nil {
			return nil, fmt.Errorf("sürüm satırı okunamadı: %w", err)
		}
		releases = append(releases, rel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sürüm listesi okunamadı: %w", err)
	}
	return releases, nil
}

// GetRelease, tek bir sürümü okur.
func (s *Store) GetRelease(ctx context.Context, appID, releaseID string) (Release, error) {
	row := s.db.QueryRowContext(ctx,
		releaseSelect+` WHERE app_id = ? AND id = ?`, appID, releaseID)

	rel, err := scanRelease(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return Release{}, fmt.Errorf("%w: %s/%s", ErrReleaseNotFound, appID, releaseID)
	case err != nil:
		return Release{}, fmt.Errorf("sürüm okunamadı: %w", err)
	}
	return rel, nil
}

const (
	defaultReleaseLimit = 20
	maxReleaseLimit     = 500
)

// Sütun sırası TEK BİR YERDE — gerekçe için appSelect'e bakın.
const releaseSelect = `
	SELECT app_id, id, seq, commit_sha, status, image_id,
	       started_at, finished_at, detail
	FROM releases`

func scanRelease(sc scanner) (Release, error) {
	var (
		rel            Release
		status         int
		started, ended int64
	)
	err := sc.Scan(
		&rel.AppID, &rel.ID, &rel.Seq, &rel.CommitSHA, &status, &rel.ImageID,
		&started, &ended, &rel.Detail,
	)
	if err != nil {
		return Release{}, err
	}
	rel.Status = ReleaseStatus(status)
	rel.StartedAt = time.Unix(0, started)
	// finished_at = 0 "bitmedi" demektir; time.Unix(0,0) 1970'i gösterir
	// ve bir arayüzde "1 Ocak 1970'te bitti" diye görünürdü.
	if ended > 0 {
		rel.FinishedAt = time.Unix(0, ended)
	}
	return rel, nil
}

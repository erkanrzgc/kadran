package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Deployment, trafiği alan sürümdür.
//
// Alan adı ve port `apps`'ten JOIN'le geliyor: ters vekil
// yapılandırmasını üretmek için gereken her şey TEK sorguda. Ayrı ayrı
// çekmek, iki sorgu arasında değişen bir alan adının yapılandırmayı
// tutarsız bırakmasına kapı açardı.
type Deployment struct {
	AppID     string
	ReleaseID string

	Domain        string
	ContainerPort uint32

	ActivatedAt time.Time
}

// ErrNoDeployment, uygulamanın canlı bir sürümü olmadığını bildirir.
var ErrNoDeployment = errors.New("uygulamanın aktif sürümü yok")

// SetActiveRelease, trafiği bir sürüme çevirir.
//
// ⚠ Bu çağrı ters vekile DOKUNMAZ; yalnızca kontrol düzlemindeki gerçeği
// günceller. Caddy'ye yükleme ayrı bir adım ve SIRASI ÖNEMLİ: önce
// buraya yazılır, sonra yapılandırma üretilip yüklenir. Ters yapılsaydı,
// arada düşen bir panelyd canlıda kayıtsız bir rota bırakırdı ve bir
// sonraki uzlaştırma onu SESSİZCE silerdi.
//
// Sürümün BUILT olduğu burada kontrol edilmiyor: şemadaki tetikleyici
// zorluyor ve orada olması, bu yolu atlayan bir çağıranın da aynı
// kısıta çarpmasını sağlıyor.
func (s *Store) SetActiveRelease(ctx context.Context, appID, releaseID string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO deployments (app_id, release_id, activated_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(app_id) DO UPDATE SET
		     release_id   = excluded.release_id,
		     activated_at = excluded.activated_at`,
		appID, releaseID, time.Now().UnixNano())
	if err != nil {
		return fmt.Errorf("aktif sürüm yazılamadı (%s/%s): %w", appID, releaseID, err)
	}
	return nil
}

// ClearActiveRelease, uygulamayı trafikten çeker.
func (s *Store) ClearActiveRelease(ctx context.Context, appID string) error {
	if _, err := s.db.ExecContext(ctx,
		`DELETE FROM deployments WHERE app_id = ?`, appID); err != nil {
		return fmt.Errorf("aktif sürüm silinemedi (%s): %w", appID, err)
	}
	return nil
}

// ActiveDeployment, tek bir uygulamanın canlı sürümünü döndürür.
func (s *Store) ActiveDeployment(ctx context.Context, appID string) (Deployment, error) {
	row := s.db.QueryRowContext(ctx, deploymentSelect+` WHERE d.app_id = ?`, appID)

	d, err := scanDeployment(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Deployment{}, fmt.Errorf("%w (%s)", ErrNoDeployment, appID)
	}
	if err != nil {
		return Deployment{}, fmt.Errorf("aktif sürüm okunamadı (%s): %w", appID, err)
	}
	return d, nil
}

// ActiveDeployments, TÜM uygulamaların canlı sürümlerini döndürür.
//
// ── Neden hepsi? ────────────────────────────────────────────────────
//
// Caddy'nin `POST /load` ucu kök nesnenin TAMAMINI değiştiriyor. Tek bir
// uygulamadan üretilmiş bir yapılandırma, diğer uygulamaların rotalarını
// SİLER — yani bir dağıtım, alakasız bir siteyi internetten düşürür.
//
// Bu yüzden ters vekil yapılandırması her seferinde BURADAN, tüm
// dağıtımlardan üretiliyor. K-054 aynı hatayı geri okuma tarafında da
// yakalıyor: eksik üretilmiş bir yapılandırma artık doğrulamadan geçmez.
//
// Sıralama BELİRLENİMLİ (app_id): aynı durumdan aynı JSON çıkmazsa
// "yüklediğim şey canlıda mı" karşılaştırması gürültüye boğulurdu.
func (s *Store) ActiveDeployments(ctx context.Context) ([]Deployment, error) {
	rows, err := s.db.QueryContext(ctx, deploymentSelect+` ORDER BY d.app_id`)
	if err != nil {
		return nil, fmt.Errorf("aktif sürümler okunamadı: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Deployment
	for rows.Next() {
		d, err := scanDeployment(rows)
		if err != nil {
			return nil, fmt.Errorf("aktif sürüm satırı çözümlenemedi: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		// Yutulmuyor: yarım okunmuş bir liste, ters vekilde EKSİK bir
		// yapılandırma demek — yani sessizce düşen siteler.
		return nil, fmt.Errorf("aktif sürümler okunurken hata: %w", err)
	}
	return out, nil
}

const deploymentSelect = `
	SELECT d.app_id, d.release_id, d.activated_at, a.domain, a.container_port
	FROM deployments d
	JOIN apps a ON a.id = d.app_id`

func scanDeployment(sc scanner) (Deployment, error) {
	var (
		d           Deployment
		activatedAt int64
	)
	if err := sc.Scan(&d.AppID, &d.ReleaseID, &activatedAt,
		&d.Domain, &d.ContainerPort); err != nil {
		return Deployment{}, err
	}
	d.ActivatedAt = time.Unix(0, activatedAt)
	return d, nil
}

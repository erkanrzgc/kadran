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

// ErrNoPreviousDeployment, geri alınacak bir sürüm olmadığını bildirir.
//
// ErrNoDeployment'tan AYRI: "hiç dağıtılmamış" ile "dağıtılmış ama geri
// alınacak öncesi yok" farklı durumlar ve kullanıcıya farklı şey
// söylenmeli. İlk dağıtımdan sonra geri alma denemesi bu ikincisidir.
var ErrNoPreviousDeployment = errors.New("geri alınacak önceki sürüm yok")

// SetActiveRelease, trafiği bir sürüme çevirir.
//
// ⚠ Bu çağrı ters vekile DOKUNMAZ; yalnızca kontrol düzlemindeki gerçeği
// günceller. Caddy'ye yükleme ayrı bir adım ve SIRASI ÖNEMLİ: önce
// buraya yazılır, sonra yapılandırma üretilip yüklenir. Ters yapılsaydı,
// arada düşen bir panelyd canlıda kayıtsız bir rota bırakırdı ve bir
// sonraki uzlaştırma onu SESSİZCE silerdi.
//
// ── Ekle-sadece: eskisi KAPATILIR, yenisi EKLENİR ───────────────────
//
// Göç 0005'ten beri `deployments` bir GEÇMİŞ. Aktivasyon artık üzerine
// yazmıyor; açık satır kapanıyor ve yeni bir satır açılıyor. İkisi TEK
// transaction'da: arada düşen bir panelyd, uygulamayı hiç aktif sürümü
// olmayan bir durumda bırakırdı ve bir sonraki uzlaştırma siteyi
// internetten düşürürdü.
//
// Sürümün BUILT olduğu burada kontrol edilmiyor: şemadaki tetikleyici
// zorluyor ve orada olması, bu yolu atlayan bir çağıranın da aynı
// kısıta çarpmasını sağlıyor.
func (s *Store) SetActiveRelease(ctx context.Context, appID, releaseID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("aktif sürüm yazılamadı (%s/%s): %w", appID, releaseID, err)
	}
	defer func() { _ = tx.Rollback() }()

	var current string
	err = tx.QueryRowContext(ctx,
		`SELECT release_id FROM deployments
		 WHERE app_id = ? AND deactivated_at IS NULL`, appID).Scan(&current)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// Aktif sürüm yok; doğrudan açılacak.
	case err != nil:
		return fmt.Errorf("açık dağıtım okunamadı (%s): %w", appID, err)
	case current == releaseID:
		// ZATEN AKTİF — hiçbir şey yapılmıyor.
		//
		// Kapatıp yeniden açmak geçmişe sahte bir giriş eklerdi ve geri
		// alma "önceki sürüm" diye AYNI sürümü bulurdu. Yani bu dal bir
		// verimlilik kısayolu değil, doğruluk şartı.
		return tx.Commit()
	}

	now := time.Now().UnixNano()

	// MAX(...) neden: sistem saati NTP düzeltmesiyle GERİ gidebilir ve
	// şemadaki `deactivated_at >= activated_at` kısıtı patlardı — yani bir
	// saat düzeltmesi dağıtımı başarısız kılardı. Sıralamanın otoritesi
	// zaten `seq`; zaman damgaları insan içindir.
	if _, err := tx.ExecContext(ctx,
		`UPDATE deployments SET deactivated_at = MAX(activated_at, ?)
		 WHERE app_id = ? AND deactivated_at IS NULL`, now, appID); err != nil {
		return fmt.Errorf("önceki dağıtım kapatılamadı (%s): %w", appID, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO deployments (app_id, release_id, activated_at)
		 VALUES (?, ?, ?)`, appID, releaseID, now); err != nil {
		return fmt.Errorf("aktif sürüm yazılamadı (%s/%s): %w", appID, releaseID, err)
	}

	return tx.Commit()
}

// ClearActiveRelease, uygulamayı trafikten çeker.
//
// Satır SİLİNMEZ, KAPATILIR. Silmek geri almanın dayandığı geçmişi yok
// ederdi: trafikten çekilmiş bir uygulamayı tekrar canlıya almak, en son
// hangi sürümün çalıştığını bilmeyi gerektirir.
func (s *Store) ClearActiveRelease(ctx context.Context, appID string) error {
	if _, err := s.db.ExecContext(ctx,
		`UPDATE deployments SET deactivated_at = MAX(activated_at, ?)
		 WHERE app_id = ? AND deactivated_at IS NULL`,
		time.Now().UnixNano(), appID); err != nil {
		return fmt.Errorf("aktif sürüm kapatılamadı (%s): %w", appID, err)
	}
	return nil
}

// PreviousActiveRelease, geri almanın hedefini döndürür: en son KAPANMIŞ
// dağıtım satırının sürümü.
//
// ── Neden `releases.seq - 1` DEĞİL ──────────────────────────────────
//
// Sürüm sırası aktivasyon geçmişi değildir. r5 canlıyken r3'e geri
// alınırsa, bir sonraki geri alma r2'ye değil r5'e gitmelidir — çünkü
// gerçekten canlı olan en son önceki sürüm odur. Bu soruyu yalnızca
// geçmiş cevaplayabilir.
//
// Sıralama `seq` ile: zaman damgası iki aktivasyonu aynı nanosaniyeye
// düşürebilir ve hedef belirsizleşirdi.
func (s *Store) PreviousActiveRelease(ctx context.Context, appID string) (string, error) {
	var releaseID string
	err := s.db.QueryRowContext(ctx,
		`SELECT release_id FROM deployments
		 WHERE app_id = ? AND deactivated_at IS NOT NULL
		 ORDER BY seq DESC LIMIT 1`, appID).Scan(&releaseID)

	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("%w (%s)", ErrNoPreviousDeployment, appID)
	}
	if err != nil {
		return "", fmt.Errorf("önceki dağıtım okunamadı (%s): %w", appID, err)
	}
	return releaseID, nil
}

// ActiveDeployment, tek bir uygulamanın canlı sürümünü döndürür.
func (s *Store) ActiveDeployment(ctx context.Context, appID string) (Deployment, error) {
	row := s.db.QueryRowContext(ctx, deploymentSelect+` AND d.app_id = ?`, appID)

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

// deploymentSelect, YALNIZCA açık (canlı) dağıtımları seçer.
//
// ⚠ `deactivated_at IS NULL` filtresi burada, sabitin İÇİNDE duruyor ve
// çağıranlara bırakılmıyor. Göç 0005'ten sonra tablo geçmiş tuttuğu için
// filtresiz bir sorgu, her uygulamanın ESKİ sürümlerini de döndürürdü;
// ters vekil yapılandırması Caddy'ye `POST /load` ile KÖK nesne olarak
// gittiği için bu, tek seferde TÜM siteleri yanlış upstream'lere
// bağlardı. Filtreyi unutmayı mümkün kılmamak, hatırlamaktan güvenli.
const deploymentSelect = `
	SELECT d.app_id, d.release_id, d.activated_at, a.domain, a.container_port
	FROM deployments d
	JOIN apps a ON a.id = d.app_id
	WHERE d.deactivated_at IS NULL`

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

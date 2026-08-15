package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrAppNotFound, istenen uygulamanın tanımlı olmadığını bildirir.
var ErrAppNotFound = errors.New("uygulama bulunamadı")

// ErrAppExists, aynı kimlikle bir uygulamanın zaten var olduğunu bildirir.
var ErrAppExists = errors.New("uygulama zaten var")

// App, kontrol düzlemindeki bir uygulama tanımıdır.
//
// Alanlar proto'daki AppSpec ile birebir eşleşir. Dönüşüm internal/api
// içinde yapılır, internal/pbconv'da DEĞİL: pbconv, panely-exec'in içe
// aktarma grafiğinde olduğu için ayrıcalıklı yüzey bütçesine yazılır
// (bkz. scripts/check-exec-surface.sh). panelyd tarafına ait bir
// dönüştürücüyü oraya koymak, ayrıcalıklı süreçle hiç ilgisi olmayan
// kodu root bütçesinden harcamak olurdu.
type App struct {
	ID string

	GitHost   string
	GitOwner  string
	GitRepo   string
	GitBranch string

	DockerfilePath string
	BuildArgs      map[string]string

	ContainerPort uint32
	Replicas      uint32
	HealthPath    string
	Domain        string

	MemoryBytes uint64
	CPUMillis   uint32
	BlkioWeight uint32

	// ReleaseSeq, şimdiye kadar üretilmiş sürüm sayısı.
	ReleaseSeq uint32

	CreatedAt time.Time
	UpdatedAt time.Time
}

// CreateApp, yeni bir uygulama tanımı yazar.
//
// Zaman damgaları çağırandan alınmaz; sunucu saatinden yazılır. Çağırana
// bırakmak, geçmişe tarihli kayıtlar üretilebilmesi demekti.
func (s *Store) CreateApp(ctx context.Context, app App) (App, error) {
	now := time.Now()
	app.CreatedAt = now
	app.UpdatedAt = now
	app.ReleaseSeq = 0

	args, err := json.Marshal(sortedArgs(app.BuildArgs))
	if err != nil {
		return App{}, fmt.Errorf("derleme argümanları serileştirilemedi: %w", err)
	}

	const q = `
		INSERT INTO apps (
			id, git_host, git_owner, git_repo, git_branch,
			dockerfile_path, build_args_json,
			container_port, replicas, health_path, domain,
			memory_bytes, cpu_millis, blkio_weight,
			release_seq, created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?, ?,?,?,?, ?,?,?, 0,?,?)`

	_, err = s.db.ExecContext(ctx, q,
		app.ID, app.GitHost, app.GitOwner, app.GitRepo, app.GitBranch,
		app.DockerfilePath, string(args),
		app.ContainerPort, app.Replicas, app.HealthPath, app.Domain,
		app.MemoryBytes, app.CPUMillis, app.BlkioWeight,
		now.UnixNano(), now.UnixNano(),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return App{}, s.explainCreateConflict(ctx, app, err)
		}
		return App{}, fmt.Errorf("uygulama yazılamadı: %w", err)
	}
	return app, nil
}

// explainCreateConflict, benzersizlik ihlalinin HANGİ kısıttan geldiğini
// veritabanına sorar.
//
// ── Neden gerekli? ───────────────────────────────────────────────────
//
// Göç 0004'ten önce `apps` tablosundaki tek benzersizlik kısıtı birincil
// anahtardı, dolayısıyla "ihlal ⇒ kimlik zaten var" çıkarımı DOĞRUYDU.
// Alan adı indeksi o çıkarımı geçersiz kıldı: aynı hata sınıfı artık iki
// sebepten doğuyor ve eski eşleme, alan adı çakışmasını "uygulama zaten
// var: <henüz-yaratılmamış-kimlik>" diye raporlardı.
//
// Bu, K-056'nın sınıfı: bir mekanizma değişince ona dayanan çıkarımlar
// sessizce yalana döner. Sürücünün hata KODU bu ayrımı taşımıyor (ikisi
// de SQLITE_CONSTRAINT_UNIQUE) ve mesaj metnine bakmak sürücü sürümüne
// bağımlılık olurdu; tek dürüst kaynak veritabanının kendisi.
func (s *Store) explainCreateConflict(ctx context.Context, app App, cause error) error {
	// Kimlik ÖNCE sorulur. İki kısıt birden ihlal edilmişse kullanıcının
	// çözmesi gereken ilk şey kimliktir: alan adını düzeltmek yaratmayı
	// yine de geçirmezdi.
	var one int
	if err := s.db.QueryRowContext(ctx,
		`SELECT 1 FROM apps WHERE id = ?`, app.ID).Scan(&one); err == nil {
		return fmt.Errorf("%w: %s", ErrAppExists, app.ID)
	}
	return domainConflict(ctx, s.db, app.Domain, app.ID, cause)
}

// GetApp, tek bir uygulamayı okur. Yoksa ErrAppNotFound döner.
func (s *Store) GetApp(ctx context.Context, id string) (App, error) {
	row := s.db.QueryRowContext(ctx, appSelect+` WHERE id = ?`, id)

	app, err := scanApp(row)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return App{}, fmt.Errorf("%w: %s", ErrAppNotFound, id)
	case err != nil:
		return App{}, fmt.Errorf("uygulama okunamadı: %w", err)
	}
	return app, nil
}

// ListApps, tanımlı uygulamaları kimlik sırasına göre döner.
func (s *Store) ListApps(ctx context.Context) ([]App, error) {
	rows, err := s.db.QueryContext(ctx, appSelect+` ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("uygulamalar okunamadı: %w", err)
	}
	defer func() { _ = rows.Close() }()

	apps := []App{}
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			return nil, fmt.Errorf("uygulama satırı okunamadı: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("uygulama listesi okunamadı: %w", err)
	}
	return apps, nil
}

// appSelect, sütun sırasını TEK BİR YERDE tutar.
//
// Sorgu başına ayrı bir liste yazmak, scanApp ile sıraların sessizce
// ayrışmasına açık olurdu: `git_owner` ve `git_repo` aynı tipte olduğu
// için yer değiştirdiklerinde derleme geçer, testler de büyük ihtimalle
// geçer, ve hata ancak yanlış depo derlendiğinde görülür.
const appSelect = `
	SELECT id, git_host, git_owner, git_repo, git_branch,
	       dockerfile_path, build_args_json,
	       container_port, replicas, health_path, domain,
	       memory_bytes, cpu_millis, blkio_weight,
	       release_seq, created_at, updated_at
	FROM apps`

func scanApp(sc scanner) (App, error) {
	var (
		app          App
		argsJSON     string
		created, upd int64
	)
	err := sc.Scan(
		&app.ID, &app.GitHost, &app.GitOwner, &app.GitRepo, &app.GitBranch,
		&app.DockerfilePath, &argsJSON,
		&app.ContainerPort, &app.Replicas, &app.HealthPath, &app.Domain,
		&app.MemoryBytes, &app.CPUMillis, &app.BlkioWeight,
		&app.ReleaseSeq, &created, &upd,
	)
	if err != nil {
		return App{}, err
	}
	if err := json.Unmarshal([]byte(argsJSON), &app.BuildArgs); err != nil {
		return App{}, fmt.Errorf("derleme argümanları çözümlenemedi: %w", err)
	}
	app.CreatedAt = time.Unix(0, created)
	app.UpdatedAt = time.Unix(0, upd)
	return app, nil
}

// sortedArgs, nil haritayı boş haritaya çevirir.
//
// encoding/json nil bir map'i "null" olarak yazar ve okurken nil'e
// çözer; şema ise DEFAULT '{}' diyor. İkisinin ayrışması, "argüman yok"
// durumunun yazma yoluna göre farklı temsil edilmesi demekti.
// (Anahtar sırası zaten encoding/json tarafından sıralanır.)
func sortedArgs(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// isUniqueViolation, hatanın birincil anahtar/benzersizlik ihlali olup
// olmadığını söyler.
//
// Mesaj metnine bakmak yerine sürücünün hata KODUNA bakılıyor: metin
// yerelleştirilebilir ve sürücü sürümleri arasında değişir, kod değişmez.
func isUniqueViolation(err error) bool {
	var serr *sqlite.Error
	if !errors.As(err, &serr) {
		return false
	}
	// Birincil (SQLITE_CONSTRAINT) ve genişletilmiş (…_PRIMARYKEY,
	// …_UNIQUE) kodların hepsi aynı sınıfı gösterir.
	switch serr.Code() {
	case sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY, sqlite3.SQLITE_CONSTRAINT_UNIQUE:
		return true
	}
	return false
}

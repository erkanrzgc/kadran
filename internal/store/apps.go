package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// ErrAppNotFound, istenen uygulamanın tanımlı olmadığını bildirir.
var ErrAppNotFound = errors.New("uygulama bulunamadı")

// ErrAppExists, aynı kimlikle bir uygulamanın zaten var olduğunu bildirir.
var ErrAppExists = errors.New("uygulama zaten var")

// VolumeMount, kalıcı bir hacmin uygulamaya nasıl bağlandığıdır.
//
// Alan adları JSON'da SABİT: sütun serileştirilmiş hâlde saklandığı için
// bir alanı yeniden adlandırmak, diskteki mevcut satırları sessizce
// okunamaz hâle getirir. Değiştirmek gerekirse göç yazılmalı.
type VolumeMount struct {
	// Name, ^[a-z0-9][a-z0-9-]{0,63}$ — executor'ın kısıtıyla aynı.
	Name string `json:"name"`
	// MountPath, konteyner İÇİNDEKİ bağlama noktasıdır.
	MountPath string `json:"mount_path"`
	// ReadOnly, bağlamanın salt-okunur olduğunu söyler.
	//
	// ⚠ Kaybolması SESSİZ BİR YETKİ GENİŞLEMESİDİR: salt-okunur olması
	// istenen bir hacim yazılabilir bağlanır ve kimse fark etmez.
	ReadOnly bool `json:"read_only,omitempty"`
}

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

	// Env, konteynere geçirilecek ortam değişkenleridir.
	//
	// ⚠ Değerleri `docker inspect` çıktısında düz metin görünür — sır
	// taşımamalıdır (göç 0006'daki gerekçeye bakın). Denetim kaydına
	// yalnızca ANAHTARLARI yazılır (audit.RedactEnv).
	//
	// Değişikliği BİR SONRAKİ dağıtımda etkili olur: Docker çalışan bir
	// konteynerin ortamını değiştiremez, bu bir tasarım tercihi değil
	// altyapının kısıtıdır.
	Env map[string]string

	// Volumes, kalıcı disk bağlamalarıdır.
	//
	// ⚠ HOST YOLU TAŞIMAZ — yalnızca hacim adı ve konteyner içindeki
	// bağlama noktası. Yolu executor kuruyor (göç 0007'deki gerekçe).
	//
	// Env gibi, değişikliği BİR SONRAKİ dağıtımda etkili olur: bağlama
	// konteyner oluşturulurken kuruluyor ve Docker onu sonradan
	// değiştiremez.
	Volumes []VolumeMount

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

	// Normalleştirme DÖNEN yapıya da yazılıyor. Yalnızca serileştirmede
	// yapılsaydı `CreateApp`'in döndürdüğü kayıt ile `GetApp`'in okuduğu
	// kayıt ayrışırdı: biri nil, diğeri boş harita. Aynı satırı iki farklı
	// şekilde tanımlayan bir API, çağıranı her ikisine de hazırlıklı
	// olmaya zorlar.
	app.Env = sortedArgs(app.Env)
	env, err := json.Marshal(app.Env)
	if err != nil {
		return App{}, fmt.Errorf("ortam değişkenleri serileştirilemedi: %w", err)
	}

	app.Volumes = sortedVolumes(app.Volumes)
	vols, err := json.Marshal(app.Volumes)
	if err != nil {
		return App{}, fmt.Errorf("hacimler serileştirilemedi: %w", err)
	}

	const q = `
		INSERT INTO apps (
			id, git_host, git_owner, git_repo, git_branch,
			dockerfile_path, build_args_json, env_json, volumes_json,
			container_port, replicas, health_path, domain,
			memory_bytes, cpu_millis, blkio_weight,
			release_seq, created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?,?,?, ?,?,?,?, ?,?,?, 0,?,?)`

	_, err = s.db.ExecContext(ctx, q,
		app.ID, app.GitHost, app.GitOwner, app.GitRepo, app.GitBranch,
		app.DockerfilePath, string(args), string(env), string(vols),
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
	       dockerfile_path, build_args_json, env_json, volumes_json,
	       container_port, replicas, health_path, domain,
	       memory_bytes, cpu_millis, blkio_weight,
	       release_seq, created_at, updated_at
	FROM apps`

func scanApp(sc scanner) (App, error) {
	var (
		app          App
		argsJSON     string
		envJSON      string
		volsJSON     string
		created, upd int64
	)
	err := sc.Scan(
		&app.ID, &app.GitHost, &app.GitOwner, &app.GitRepo, &app.GitBranch,
		&app.DockerfilePath, &argsJSON, &envJSON, &volsJSON,
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
	if err := json.Unmarshal([]byte(envJSON), &app.Env); err != nil {
		return App{}, fmt.Errorf("ortam değişkenleri çözümlenemedi: %w", err)
	}
	// "{}" nil DEĞİL boş harita üretir, ama "null" nil üretir — göç
	// öncesi yazılmış bir satır ya da elle yapılmış bir müdahale bunu
	// döndürebilir. Normalleştirme burada kapanıyor ki okuyucuların
	// hiçbiri nil kontrolü yapmak zorunda kalmasın.
	app.Env = sortedArgs(app.Env)
	if err := json.Unmarshal([]byte(volsJSON), &app.Volumes); err != nil {
		return App{}, fmt.Errorf("hacimler çözümlenemedi: %w", err)
	}
	app.Volumes = sortedVolumes(app.Volumes)
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

// sortedVolumes, nil dilimi boş dilime çevirir ve ADA göre sıralar.
//
// ── Neden sıralama GEREKLİ? ──────────────────────────────────────────
//
// Haritalarda `encoding/json` anahtarları kendisi sıralıyor, dilimlerde
// SIRALAMIYOR. Sıralamadan yazmak, aynı hacim kümesinin çağrı sırasına
// göre farklı JSON üretmesi demekti: `-volume a -volume b` ile
// `-volume b -volume a` diskte FARKLI satırlar bırakır, `app show`
// çıktısı değişir ve iki kaydı karşılaştıran her şey yanlış "değişti"
// der.
//
// Ada göre sıralamak aynı kümeyi daima aynı bayta indirger.
func sortedVolumes(v []VolumeMount) []VolumeMount {
	if v == nil {
		return []VolumeMount{}
	}
	out := make([]VolumeMount, len(v))
	copy(out, v)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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

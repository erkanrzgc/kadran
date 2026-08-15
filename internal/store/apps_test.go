package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"modernc.org/sqlite"
)

// newAppStore, disk üzerinde bir depo açar.
//
// ":memory:" KULLANILMIYOR: eşzamanlılık testi gerçek WAL davranışına ve
// _txlock=immediate'e bakıyor, ve bunlar yalnızca dosya DSN'inde devrede
// (buildDSN'e bakın). Bellek DSN'i farklı bir yol izliyor, yani orada
// geçen bir test üretimdeki davranış hakkında bir şey söylemezdi.
func newAppStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("depo açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleApp(id string) App {
	return App{
		ID:             id,
		GitHost:        "github.com",
		GitOwner:       "erkanrzgc",
		GitRepo:        "panely",
		GitBranch:      "main",
		DockerfilePath: "Dockerfile",
		BuildArgs:      map[string]string{"NODE_ENV": "production"},
		ContainerPort:  8080,
		Replicas:       2,
		HealthPath:     "/healthz",
		// Alan adı KİMLİKTEN türetiliyor. Sabit "example.com" idi ve göç
		// 0004 gelince iki uygulama yaratan her test kırıldı — kısıtın
		// gerçekten zorlandığının kanıtı. Çakışmayı SINAYAN testler alanı
		// açıkça ayarlar; geri kalanının onunla işi yok.
		Domain:      id + ".example.com",
		MemoryBytes: 256 << 20,
		CPUMillis:   500,
		BlkioWeight: 500,
	}
}

const testSHA = "0123456789abcdef0123456789abcdef01234567"

func TestCreateAppRoundTrips(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	want := sampleApp("blog")
	created, err := s.CreateApp(ctx, want)
	if err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	if created.CreatedAt.IsZero() {
		t.Error("created_at yazılmamış")
	}

	got, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}

	// Alanların TEK TEK karşılaştırılması kasıtlı: appSelect'teki sütun
	// sırası ile scanApp'inki ayrışırsa aynı tipteki iki alan (git_owner
	// ↔ git_repo) sessizce yer değiştirir ve yalnızca böyle bir
	// karşılaştırma bunu görür.
	if got.GitOwner != want.GitOwner || got.GitRepo != want.GitRepo {
		t.Errorf("owner/repo karıştı: owner=%q repo=%q", got.GitOwner, got.GitRepo)
	}
	if got.GitHost != want.GitHost || got.GitBranch != want.GitBranch {
		t.Errorf("host/branch karıştı: host=%q branch=%q", got.GitHost, got.GitBranch)
	}
	if got.ContainerPort != want.ContainerPort || got.Replicas != want.Replicas {
		t.Errorf("port/replika karıştı: port=%d replicas=%d", got.ContainerPort, got.Replicas)
	}
	if got.MemoryBytes != want.MemoryBytes || got.CPUMillis != want.CPUMillis ||
		got.BlkioWeight != want.BlkioWeight {
		t.Errorf("limitler karıştı: %+v", got)
	}
	if got.HealthPath != want.HealthPath || got.Domain != want.Domain {
		t.Errorf("health/domain karıştı: health=%q domain=%q", got.HealthPath, got.Domain)
	}
	if got.BuildArgs["NODE_ENV"] != "production" {
		t.Errorf("derleme argümanları kayboldu: %v", got.BuildArgs)
	}
}

func TestCreateAppRejectsDuplicate(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("ilk yaratma başarısız: %v", err)
	}
	_, err := s.CreateApp(ctx, sampleApp("blog"))
	if !errors.Is(err, ErrAppExists) {
		t.Fatalf("ikinci yaratma ErrAppExists dönmeliydi, döndü: %v", err)
	}
}

func TestGetAppReportsMissing(t *testing.T) {
	s := newAppStore(t)
	if _, err := s.GetApp(context.Background(), "yok"); !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("ErrAppNotFound bekleniyordu, geldi: %v", err)
	}
}

// TestSchemaRejectsMalformedApps, göç 0002'deki CHECK'lerin GERÇEKTEN
// ateşlendiğini doğrular.
//
// Bu kontroller Go tarafındaki doğrulayıcıların KOPYASI değil, ikinci
// katman. Kopya olsalardı bile sınanmaları gerekirdi: yazılıp hiç
// ateşlenmeyen bir kısıt, olduğu sanılan ama olmayan bir savunmadır.
func TestSchemaRejectsMalformedApps(t *testing.T) {
	cases := []struct {
		name  string
		mutil func(*App)
	}{
		{"büyük harfli kimlik", func(a *App) { a.ID = "Blog" }},
		{"rakamla başlayan kimlik", func(a *App) { a.ID = "1blog" }},
		{"nokta içeren kimlik", func(a *App) { a.ID = "blog.app" }},
		{"eğik çizgi içeren kimlik", func(a *App) { a.ID = "a/b" }},
		{"boş kimlik", func(a *App) { a.ID = "" }},
		{"çok uzun kimlik", func(a *App) { a.ID = strings.Repeat("a", 33) }},
		{"sıfır port", func(a *App) { a.ContainerPort = 0 }},
		{"aralık dışı port", func(a *App) { a.ContainerPort = 70000 }},
		{"sıfır replika", func(a *App) { a.Replicas = 0 }},
		{"limitsiz bellek", func(a *App) { a.MemoryBytes = 0 }},
		{"limitsiz cpu", func(a *App) { a.CPUMillis = 0 }},
		{"aralık dışı blkio", func(a *App) { a.BlkioWeight = 5 }},
		{"boş git host", func(a *App) { a.GitHost = "" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAppStore(t)
			app := sampleApp("blog")
			tc.mutil(&app)

			if _, err := s.CreateApp(context.Background(), app); err == nil {
				t.Fatal("şema bozuk uygulamayı KABUL ETTİ — CHECK ateşlemiyor")
			}
		})
	}
}

func TestStartReleaseNumbersSequentially(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}

	for want := uint32(1); want <= 3; want++ {
		rel, err := s.StartRelease(ctx, "blog", testSHA)
		if err != nil {
			t.Fatalf("sürüm açılamadı: %v", err)
		}
		if rel.Seq != want {
			t.Errorf("sıra numarası %d, beklenen %d", rel.Seq, want)
		}
		if rel.ID != ReleaseID(want) {
			t.Errorf("sürüm kimliği %q, beklenen %q", rel.ID, ReleaseID(want))
		}
		if rel.Status != ReleaseBuilding {
			t.Errorf("yeni sürüm BUILDING olmalı, %d", rel.Status)
		}
	}

	app, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if app.ReleaseSeq != 3 {
		t.Errorf("release_seq %d, beklenen 3", app.ReleaseSeq)
	}
}

func TestStartReleaseRejectsUnknownApp(t *testing.T) {
	s := newAppStore(t)
	_, err := s.StartRelease(context.Background(), "yok", testSHA)
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("ErrAppNotFound bekleniyordu, geldi: %v", err)
	}
}

// TestConcurrentReleasesNeverShareAnID, eşzamanlı dağıtımların aynı sürüm
// kimliğini üretemeyeceğini doğrular.
//
// Bu, "iyi olur" değil DOĞRULUK meselesi: sürüm kimliği host tarafında
// konteyner etiketidir. İki sürüm aynı kimliği alsaydı, birinin
// konteynerlerini durdurmak diğerininkini de durdururdu.
func TestConcurrentReleasesNeverShareAnID(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}

	const n = 16
	var (
		mu   sync.Mutex
		seen = map[string]int{}
		wg   sync.WaitGroup
	)
	wg.Add(n)
	for range n {
		go func() {
			defer wg.Done()
			rel, err := s.StartRelease(ctx, "blog", testSHA)
			if err != nil {
				return
			}
			mu.Lock()
			seen[rel.ID]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(seen) != n {
		t.Fatalf("%d sürüm açıldı, %d benzersiz kimlik üretildi", n, len(seen))
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("kimlik %q %d kez üretildi", id, count)
		}
	}
}

// TestFinishReleaseRequiresImageID, boş imaj kimliğinin GO KATMANINDA
// reddedildiğini doğrular.
//
// ── Bu test bir kez YANLIŞ SEBEPLE geçti ───────────────────────────
//
// İlk hâli yalnızca "hata döndü mü" diye bakıyordu. Mutasyon sınamasında
// FinishRelease'in kontrolü SİLİNDİĞİNDE test YİNE GEÇTİ: çağrı bu kez
// veritabanına ulaşıyor ve göç 0002'deki CHECK onu reddediyordu. Doğru
// sonuç, yanlış mekanizma — testin koruduğunu sandığı şey korunmuyordu.
//
// Ayırt edici ölçüt "hangi KATMAN reddetti" olmalı. Veritabanından gelen
// bir ret, hata zincirinde *sqlite.Error taşır; Go kontrolünün ürettiği
// hata taşımaz. Şema katmanının kendi kanıtı ayrıca
// TestSchemaForbidsBuiltWithoutImageID'de.
func TestFinishReleaseRequiresImageID(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	rel, err := s.StartRelease(ctx, "blog", testSHA)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}

	err = s.FinishRelease(ctx, "blog", rel.ID, "")
	if err == nil {
		t.Fatal("imaj kimliği olmadan BUILT kabul edildi")
	}

	var serr *sqlite.Error
	if errors.As(err, &serr) {
		t.Fatalf("ret veritabanından geldi, Go kontrolünden değil: %v\n"+
			"FinishRelease isteği hiç tele çıkarmamalıydı", err)
	}

	// Sürüm hâlâ BUILDING olmalı: reddedilen çağrı durumu değiştirmemeli.
	got, err := s.GetRelease(ctx, "blog", rel.ID)
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if got.Status != ReleaseBuilding {
		t.Errorf("reddedilen çağrı durumu değiştirdi: %d", got.Status)
	}
}

// TestSchemaForbidsBuiltWithoutImageID, K-042'nin ŞEMA katmanında
// zorlandığını kanıtlar.
//
// ── Neden Go doğrulayıcısını atlıyor? ──────────────────────────────
//
// FinishRelease zaten boş imaj kimliğini reddediyor ve bunun testi
// yukarıda. Ama o test yalnızca Go tarafını kanıtlar: kontrolü silsem
// veritabanı hâlâ tutuyor mu, söylemez. Bu test doğrudan SQL yazarak Go
// katmanını devreden çıkarıyor — uygulama katmanındaki bir hatanın
// (veya gelecekteki bir refactor'ün) "derlendi ama kanıtı yok" satırı
// yazamayacağını gösteren tek kanıt budur.
func TestSchemaForbidsBuiltWithoutImageID(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	rel, err := s.StartRelease(ctx, "blog", testSHA)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}

	_, err = s.DB().ExecContext(ctx,
		`UPDATE releases SET status = 2, image_id = '', finished_at = 1
		 WHERE app_id = ? AND id = ?`, "blog", rel.ID)
	if err == nil {
		t.Fatal("şema, imaj kimliği olmayan BUILT satırını KABUL ETTİ — " +
			"K-042 şema katmanında zorlanmıyor")
	}
}

func TestSealedReleaseCannotBeResealed(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	rel, err := s.StartRelease(ctx, "blog", testSHA)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}
	if err := s.FinishRelease(ctx, "blog", rel.ID, "sha256:abc"); err != nil {
		t.Fatalf("sürüm mühürlenemedi: %v", err)
	}

	// Geç gelen bir hata, başarılı bir sürümü FAILED'a çeviremez.
	if err := s.FailRelease(ctx, "blog", rel.ID, "geç gelen hata"); err == nil {
		t.Fatal("mühürlenmiş sürüm yeniden mühürlendi")
	}

	got, err := s.GetRelease(ctx, "blog", rel.ID)
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if got.Status != ReleaseBuilt || got.ImageID != "sha256:abc" {
		t.Errorf("başarılı sürüm bozuldu: status=%d image=%q", got.Status, got.ImageID)
	}
	if got.FinishedAt.IsZero() {
		t.Error("finished_at yazılmamış")
	}
}

func TestFailedReleaseKeepsNoImageID(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	rel, err := s.StartRelease(ctx, "blog", testSHA)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}
	if err := s.FailRelease(ctx, "blog", rel.ID, "derleme öldü"); err != nil {
		t.Fatalf("sürüm başarısız işaretlenemedi: %v", err)
	}

	got, err := s.GetRelease(ctx, "blog", rel.ID)
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if got.Status != ReleaseFailed {
		t.Errorf("durum %d, beklenen FAILED", got.Status)
	}
	if got.ImageID != "" {
		t.Errorf("başarısız sürümde imaj kimliği var: %q", got.ImageID)
	}
	if got.Detail != "derleme öldü" {
		t.Errorf("detay kaybedildi: %q", got.Detail)
	}
}

func TestListReleasesIsNewestFirst(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	for range 5 {
		if _, err := s.StartRelease(ctx, "blog", testSHA); err != nil {
			t.Fatalf("sürüm açılamadı: %v", err)
		}
	}

	got, err := s.ListReleases(ctx, "blog", 3)
	if err != nil {
		t.Fatalf("sürümler okunamadı: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("%d sürüm döndü, 3 bekleniyordu", len(got))
	}
	for i, want := range []uint32{5, 4, 3} {
		if got[i].Seq != want {
			t.Errorf("%d. sürüm seq=%d, beklenen %d", i, got[i].Seq, want)
		}
	}
}

// TestBuildingReleaseHasNoFinishTime, yarıda kalmış bir derlemenin
// "1 Ocak 1970'te bitti" gibi görünmediğini doğrular.
func TestBuildingReleaseHasNoFinishTime(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
	rel, err := s.StartRelease(ctx, "blog", testSHA)
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}

	got, err := s.GetRelease(ctx, "blog", rel.ID)
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if !got.FinishedAt.IsZero() {
		t.Errorf("derlenmekte olan sürümün bitiş zamanı var: %v", got.FinishedAt)
	}
}

// TestSchemaRejectsMalformedReleases, sürüm satırlarındaki CHECK'lerin de
// ateşlendiğini doğrular. Doğrudan SQL: Go katmanı bu değerleri zaten
// üretmiyor, sınanan şey ŞEMANIN kendisi.
func TestSchemaRejectsMalformedReleases(t *testing.T) {
	cases := []struct {
		name string
		cols string
		vals []any
	}{
		{
			"kısa commit sha",
			"app_id,id,seq,commit_sha,status,started_at",
			[]any{"blog", "rx", 99, "abc123", 1, 1},
		},
		{
			"büyük harfli commit sha",
			"app_id,id,seq,commit_sha,status,started_at",
			[]any{"blog", "rx", 99, strings.ToUpper(testSHA), 1, 1},
		},
		{
			"bilinmeyen uygulama (yabancı anahtar)",
			"app_id,id,seq,commit_sha,status,started_at",
			[]any{"yok", "rx", 99, testSHA, 1, 1},
		},
		{
			"geçersiz durum",
			"app_id,id,seq,commit_sha,status,started_at",
			[]any{"blog", "rx", 99, testSHA, 7, 1},
		},
		{
			"biten sürümde bitiş zamanı yok",
			"app_id,id,seq,commit_sha,status,image_id,started_at,finished_at",
			[]any{"blog", "rx", 99, testSHA, 3, "", 1, 0},
		},
		{
			"derlenirken imaj kimliği var",
			"app_id,id,seq,commit_sha,status,image_id,started_at",
			[]any{"blog", "rx", 99, testSHA, 1, "sha256:abc", 1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newAppStore(t)
			ctx := context.Background()
			if _, err := s.CreateApp(ctx, sampleApp("blog")); err != nil {
				t.Fatalf("uygulama yaratılamadı: %v", err)
			}

			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(tc.vals)), ",")
			q := fmt.Sprintf("INSERT INTO releases (%s) VALUES (%s)", tc.cols, placeholders)

			if _, err := s.DB().ExecContext(ctx, q, tc.vals...); err == nil {
				t.Fatal("şema bozuk sürüm satırını KABUL ETTİ")
			}
		})
	}
}

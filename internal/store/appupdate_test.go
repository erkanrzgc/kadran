package store

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mustCreate, testin ilgilendiği şeye odaklanabilmesi için yaratmayı
// kısaltır.
func mustCreate(t *testing.T, s *Store, app App) App {
	t.Helper()
	created, err := s.CreateApp(context.Background(), app)
	if err != nil {
		t.Fatalf("uygulama yaratılamadı (%s): %v", app.ID, err)
	}
	return created
}

func strptr(s string) *string { return &s }
func u32ptr(v uint32) *uint32 { return &v }

// mustUpdate, güncellemeyi koşturur ve DİSKTEKİ hâli döndürür.
//
// ── Neden dönen struct'a bakmak YETMEZ? ──────────────────────────────
//
// UpdateApp satırı transaction içinde okuyor, bellekte değiştiriyor ve
// geri yazıyor; döndürdüğü struct bu BELLEK kopyasıdır. Dolayısıyla
// SQL cümlesinin diske ne yazdığını hiç yansıtmayabilir.
//
// Bu teorik değil, ÖLÇÜLDÜ: `release_seq = 0` mutasyonu uygulandığında —
// yani sayaç diskte gerçekten sıfırlandığında — yalnızca dönen değere
// bakan test YEŞİL kaldı. Sayaç bellekteki kopyada hâlâ 3'tü.
//
// Bu yüzden her iddia diskten yeniden okunan değere yapılıyor, ve ikisinin
// AYNI olduğu ayrıca doğrulanıyor: ayrışırlarsa çağıran, yazılmamış bir
// durumu doğru sanır.
func mustUpdate(t *testing.T, s *Store, id string, upd AppUpdate) App {
	t.Helper()
	ctx := context.Background()

	returned, err := s.UpdateApp(ctx, id, upd)
	if err != nil {
		t.Fatalf("güncelleme başarısız (%s): %v", id, err)
	}
	persisted, err := s.GetApp(ctx, id)
	if err != nil {
		t.Fatalf("güncellenen uygulama okunamadı (%s): %v", id, err)
	}

	if returned.Domain != persisted.Domain ||
		returned.HealthPath != persisted.HealthPath ||
		returned.GitBranch != persisted.GitBranch ||
		returned.Replicas != persisted.Replicas ||
		returned.ReleaseSeq != persisted.ReleaseSeq ||
		returned.ContainerPort != persisted.ContainerPort {
		t.Fatalf("dönen değer diskten AYRIŞIYOR:\n  dönen   = %+v\n  diskte  = %+v",
			returned, persisted)
	}
	return persisted
}

// TestUpdateAppTouchesOnlyTheNamedFields, kısmi güncellemenin ASIL
// gerekçesini sınar.
//
// Boş dize hem `domain` hem `health_path` için GEÇERLİ bir değer ("vekilde
// görünme", "yoklama yapma"). Tam-tanım-değiştirme modelinde, alanı
// belirtmeyen bir istemci onu SESSİZCE temizlerdi. `replicas` aynı hatayı
// yapsa doğrulayıcıya çarpıp gürültülü ölürdü — yani en tehlikeli iki alan,
// tam da sessizce kaybolabilecek olanlar.
func TestUpdateAppTouchesOnlyTheNamedFields(t *testing.T) {
	s := newAppStore(t)

	mustCreate(t, s, sampleApp("blog"))

	got := mustUpdate(t, s, "blog", AppUpdate{Domain: strptr("yeni.example.com")})

	if got.Domain != "yeni.example.com" {
		t.Errorf("alan adı değişmedi: %q", got.Domain)
	}
	// Dokunulmayan alanların HEPSİ korunmalı.
	if got.HealthPath != "/healthz" {
		t.Errorf("health_path silindi: %q", got.HealthPath)
	}
	if got.GitBranch != "main" {
		t.Errorf("git_branch silindi: %q", got.GitBranch)
	}
	if got.Replicas != 2 {
		t.Errorf("replicas değişti: %d", got.Replicas)
	}
	if got.ContainerPort != 8080 {
		t.Errorf("container_port değişti: %d", got.ContainerPort)
	}
	if got.BuildArgs["NODE_ENV"] != "production" {
		t.Errorf("derleme argümanları kayboldu: %v", got.BuildArgs)
	}
	if got.MemoryBytes != 256<<20 || got.CPUMillis != 500 || got.BlkioWeight != 500 {
		t.Errorf("limitler değişti: %+v", got)
	}
}

// TestUpdateAppCanClearOptionalFields, "dokunma" ile "temizle" ayrımının
// GERÇEKTEN temsil edilebildiğini sınar. Bir üstteki test ancak bu testle
// birlikte anlamlı: yalnızca korumayı ölçseydi, hiçbir şey yazmayan bir
// uygulama da geçerdi.
func TestUpdateAppCanClearOptionalFields(t *testing.T) {
	s := newAppStore(t)

	mustCreate(t, s, sampleApp("blog"))

	got := mustUpdate(t, s, "blog", AppUpdate{
		Domain:     strptr(""),
		HealthPath: strptr(""),
	})
	if got.Domain != "" {
		t.Errorf("alan adı temizlenmedi: %q", got.Domain)
	}
	if got.HealthPath != "" {
		t.Errorf("health_path temizlenmedi: %q", got.HealthPath)
	}
}

// TestUpdateAppRejectsDomainOwnedByAnotherApp, göç 0004'ün kapattığı
// deliğin KULLANICIYA görünen yüzüdür.
func TestUpdateAppRejectsDomainOwnedByAnotherApp(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	first := sampleApp("portfolio")
	first.Domain = "panely.example.com"
	mustCreate(t, s, first)

	second := sampleApp("blog")
	second.Domain = "blog.example.com"
	mustCreate(t, s, second)

	_, err := s.UpdateApp(ctx, "blog", AppUpdate{Domain: strptr("panely.example.com")})
	if !errors.Is(err, ErrDomainTaken) {
		t.Fatalf("ErrDomainTaken bekleniyordu, geldi: %v", err)
	}
	// ⚠ NEGATİF İDDİA ZORUNLU. İkisi de SQLite'ta aynı kısıt sınıfından
	// (SQLITE_CONSTRAINT_UNIQUE) doğuyor; ayrım yapmayan bir uygulama
	// ErrAppExists dönerdi ve yukarıdaki iddia — eğer ErrDomainTaken onu
	// sarmalıyorsa — yine geçerdi. Bu satır olmadan test hiçbir şey
	// korumaz.
	if errors.Is(err, ErrAppExists) {
		t.Errorf("alan adı çakışması, kimlik çakışması gibi raporlandı: %v", err)
	}
	// Hata, çakışmanın SAHİBİNİ söylemeli — aksi hâlde kullanıcı hangi
	// uygulamayı düzelteceğini bulmak için elle arama yapar.
	if !strings.Contains(err.Error(), "portfolio") {
		t.Errorf("hata çakışan uygulamayı adlandırmıyor: %v", err)
	}

	// Reddedilen güncelleme HİÇBİR ŞEY yazmamalı.
	after, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if after.Domain != "blog.example.com" {
		t.Errorf("reddedilen güncelleme yine de yazdı: %q", after.Domain)
	}
}

// TestUpdateAppDoesNotCollideWithItself, kendi kendini çakışma sanma
// hatasını yakalar: `WHERE domain = ?` sorgusu uygulamanın KENDİ satırını
// da bulur. Bu hata gözden kaçmaya çok müsait — alan adını değiştirmeyen
// her güncelleme, yani günlük kullanımın çoğu, ilk denemede ölürdü.
func TestUpdateAppDoesNotCollideWithItself(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	own := mustCreate(t, s, sampleApp("blog")).Domain

	// 1) Alan adı hiç belirtilmeden başka bir alan güncelleniyor.
	if _, err := s.UpdateApp(ctx, "blog", AppUpdate{GitBranch: strptr("develop")}); err != nil {
		t.Fatalf("alan adına dokunmayan güncelleme reddedildi: %v", err)
	}
	// 2) Alan adı KENDİ mevcut değerine ayarlanıyor.
	got := mustUpdate(t, s, "blog", AppUpdate{Domain: &own})
	if got.Domain != own || got.GitBranch != "develop" {
		t.Errorf("beklenmeyen durum: domain=%q branch=%q", got.Domain, got.GitBranch)
	}
}

// TestUpdateAppPreservesReleaseSeq, sürüm sayacının korunduğunu sınar.
//
// Sayaç sıfırlanırsa bir sonraki sürüm yine "r1" adını alır ve host
// tarafında VAR OLAN konteynerleri adresler: iki farklı commit, aynı ad.
func TestUpdateAppPreservesReleaseSeq(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	mustCreate(t, s, sampleApp("blog"))
	for i := 0; i < 3; i++ {
		if _, err := s.StartRelease(ctx, "blog", testSHA); err != nil {
			t.Fatalf("sürüm açılamadı: %v", err)
		}
	}

	got := mustUpdate(t, s, "blog", AppUpdate{Domain: strptr("yeni.example.com")})
	if got.ReleaseSeq != 3 {
		t.Fatalf("release_seq %d oldu, 3 olmalıydı — bir sonraki sürüm var olan bir adı alırdı",
			got.ReleaseSeq)
	}
}

// TestUpdateAppMovesUpdatedAtButNotCreatedAt, iki zaman damgasının
// karıştırılmadığını sınar.
//
// ⚠ İki `time.Now()` çağrısının farkına BAKMIYOR. İlk yazımı öyleydi ve
// kırmızı verdi: bu makinede ardışık iki çağrı AYNI nanosaniyeyi döndürdü
// (ölçüldü, fark = 0s — Windows sistem saati ~15ms'de bir ilerliyor).
// Araya `Sleep` koymak testi saat çözünürlüğüne bağımlı bırakırdı; bunun
// yerine damga BİLİNEN eski bir değere çekiliyor, yani iddia saatten
// tamamen bağımsız.
func TestUpdateAppMovesUpdatedAtButNotCreatedAt(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	created := mustCreate(t, s, sampleApp("blog"))

	past := time.Now().Add(-time.Hour).UnixNano()
	if _, err := s.DB().ExecContext(ctx,
		`UPDATE apps SET updated_at = ? WHERE id = 'blog'`, past); err != nil {
		t.Fatalf("damga geriye alınamadı: %v", err)
	}

	got, err := s.UpdateApp(ctx, "blog", AppUpdate{GitBranch: strptr("develop")})
	if err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}
	if !got.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("created_at değişti: %v → %v", created.CreatedAt, got.CreatedAt)
	}
	if got.UpdatedAt.UnixNano() <= past {
		t.Errorf("updated_at ilerlemedi: %v (geri alınan değer %v)",
			got.UpdatedAt, time.Unix(0, past))
	}

	// Diskteki değer de ilerlemiş olmalı: yalnızca dönen struct'a bakmak,
	// belleği güncelleyip veritabanına yazmayan bir uygulamayı geçirirdi.
	reread, err := s.GetApp(ctx, "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if reread.UpdatedAt.UnixNano() <= past {
		t.Errorf("updated_at diske yazılmadı: %v", reread.UpdatedAt)
	}
	if reread.GitBranch != "develop" {
		t.Errorf("dal diske yazılmadı: %q", reread.GitBranch)
	}
}

func TestUpdateAppReportsMissingApp(t *testing.T) {
	s := newAppStore(t)
	_, err := s.UpdateApp(context.Background(), "yok", AppUpdate{GitBranch: strptr("main")})
	if !errors.Is(err, ErrAppNotFound) {
		t.Fatalf("ErrAppNotFound bekleniyordu, geldi: %v", err)
	}
}

// TestCreateAppDoesNotBlameTheIdWhenTheDomainIsTaken, göç 0004'ün
// YARATTIĞI yeni yalanı kapatır.
//
// İndeks eklenmeden önce `apps` tablosunda tek benzersizlik kısıtı
// birincil anahtardı, dolayısıyla "benzersizlik ihlali ⇒ kimlik zaten
// var" çıkarımı doğruydu. İndeks o çıkarımı geçersiz kıldı: artık aynı
// hata sınıfı iki farklı sebepten doğuyor ve eski eşleme, alan adı
// çakışmasını "uygulama zaten var: <yeni-kimlik>" diye raporlardı —
// hem yanlış hem de yanlış alanı gösteren bir mesaj.
func TestCreateAppDoesNotBlameTheIdWhenTheDomainIsTaken(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	first := sampleApp("portfolio")
	first.Domain = "panely.example.com"
	mustCreate(t, s, first)

	second := sampleApp("yepyeni")
	second.Domain = "panely.example.com"

	_, err := s.CreateApp(ctx, second)
	if err == nil {
		t.Fatal("alan adı çakışması kabul edildi")
	}
	if errors.Is(err, ErrAppExists) {
		t.Errorf("var olmayan bir kimlik için 'zaten var' dendi: %v", err)
	}
	if !errors.Is(err, ErrDomainTaken) {
		t.Errorf("ErrDomainTaken bekleniyordu, geldi: %v", err)
	}
	if !strings.Contains(err.Error(), "portfolio") {
		t.Errorf("hata çakışan uygulamayı adlandırmıyor: %v", err)
	}
}

// TestCreateAppStillReportsDuplicateIds, yukarıdaki düzeltmenin eski
// davranışı BOZMADIĞINI sınar. Bu testin varlığı kasıtlı: alan adı
// ayrımını eklerken kimlik çakışmasını da ErrDomainTaken'a çevirmek kolay
// bir hata ve tek başına yukarıdaki test bunu görmez.
func TestCreateAppStillReportsDuplicateIds(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	app := sampleApp("blog")
	app.Domain = "" // Alan adını devre dışı bırak: çakışma yalnızca kimlikte.
	mustCreate(t, s, app)

	_, err := s.CreateApp(ctx, app)
	if !errors.Is(err, ErrAppExists) {
		t.Fatalf("ErrAppExists bekleniyordu, geldi: %v", err)
	}
	if errors.Is(err, ErrDomainTaken) {
		t.Errorf("kimlik çakışması, alan adı çakışması gibi raporlandı: %v", err)
	}
}

// TestEmptyDomainsDoNotCollide, göçün KISMİ indeks olmasının sebebini
// sınar. Düz bir UNIQUE indeks, alan adı olmayan ikinci uygulamayı
// reddederdi — ve o durum yaygın (yalnızca iç ağdan erişilen iş yükü).
func TestEmptyDomainsDoNotCollide(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	for _, id := range []string{"bir", "iki", "uc"} {
		app := sampleApp(id)
		app.Domain = ""
		if _, err := s.CreateApp(ctx, app); err != nil {
			t.Fatalf("alan adsız uygulama %q reddedildi: %v", id, err)
		}
	}
}

// TestMigrationFailsLegiblyOnPreexistingDuplicates, göç 0004'ün EN KÖTÜ
// senaryosunu sınar: indeks eklenmeden önce yinelenen alan adı taşıyan bir
// veritabanı.
//
// ── Neden bu senaryo ayrıca sınanmalı? ───────────────────────────────
//
// Başarısız göç `schema_migrations`'a yazılmaz (applyMigration tek
// transaction), yani panelyd HER AÇILIŞTA aynı yerde ölür. Ve onarım
// aracı — `panely app update` — o panelyd'nin içinde. Yani bu, aracın
// kendisini kilitlediği bir kilitlenmedir; kurtarma yolu hostta elle
// sqlite3.
//
// Bu depoda o durum ÖLÇÜLDÜ ve yok (canlı veritabanının kopyasına indeks
// uygulandı: üç alan adı, üçü de farklı). Ama gelecekte 0004'ten önceki
// bir yedekten dönen biri için hâlâ mümkün, ve o kişinin gördüğü mesaj
// hangi göçün ve hangi ALANIN sorun çıkardığını söylemeli.
func TestMigrationFailsLegiblyOnPreexistingDuplicates(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/panely.db"

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("depo açılamadı: %v", err)
	}

	first := sampleApp("portfolio")
	first.Domain = "cakisan.example.com"
	mustCreate(t, s, first)

	// Göçü geri sar: indeksi düşür ve uygulanmış işaretini sil.
	for _, q := range []string{
		`DROP INDEX apps_domain_unique`,
		`DELETE FROM schema_migrations WHERE version = '0004_app_domain_unique.sql'`,
	} {
		if _, err := s.DB().ExecContext(ctx, q); err != nil {
			t.Fatalf("göç geri sarılamadı (%s): %v", q, err)
		}
	}

	// KONTROL GRUBU: indeks gerçekten düştüyse yinelenen alan adı artık
	// kabul edilmeli. Edilmiyorsa aşağıdaki asıl iddia yanlış sebepten
	// geçerdi — yani hiçbir şey ölçmezdi.
	second := sampleApp("blog")
	second.Domain = "cakisan.example.com"
	if _, err := s.CreateApp(ctx, second); err != nil {
		t.Fatalf("indeks düşürüldüğü hâlde yinelenen alan adı reddedildi — "+
			"bu testin düzeneği bozuk, sonucu hiçbir şey kanıtlamaz: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("depo kapatılamadı: %v", err)
	}

	// Yeniden açılış 0004'ü tekrar uygulamaya çalışacak.
	reopened, err := Open(ctx, path)
	if err == nil {
		_ = reopened.Close()
		t.Fatal("yinelenen alan adı taşıyan veritabanı sorunsuz açıldı — " +
			"göç 0004 hiçbir şey zorlamıyor")
	}
	for _, want := range []string{"0004_app_domain_unique", "domain"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("hata %q içermiyor, operatör nereye bakacağını bilemez: %v", want, err)
		}
	}
}

package api

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

func TestCreateAppRoundTripsThroughGetApp(t *testing.T) {
	srv, _ := newDeployServer(t, &fakeExec{})

	spec := testSpec()
	spec.Domain = "blog.example.com"
	spec.BuildArgs = map[string]string{"NODE_ENV": "production"}
	mustCreateApp(t, srv, spec)

	resp, err := srv.GetApp(context.Background(), &panelyv1.GetAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}

	got := resp.GetApp().GetSpec()
	if got.GetGitOwner() != "erkanrzgc" || got.GetGitRepo() != "panely" {
		t.Errorf("owner/repo karıştı: %q / %q", got.GetGitOwner(), got.GetGitRepo())
	}
	if got.GetDomain() != "blog.example.com" {
		t.Errorf("domain = %q", got.GetDomain())
	}
	if got.GetBuildArgs()["NODE_ENV"] != "production" {
		t.Errorf("derleme argümanları kayboldu: %v", got.GetBuildArgs())
	}
	if l := got.GetLimits(); l.GetMemoryBytes() != 256<<20 || l.GetCpuMillis() != 500 {
		t.Errorf("limitler kayboldu: %v", l)
	}
	if resp.GetApp().GetCreatedAt() == nil {
		t.Error("created_at boş")
	}
}

func TestCreateAppRejectsDuplicate(t *testing.T) {
	srv, _ := newDeployServer(t, &fakeExec{})
	mustCreateApp(t, srv, testSpec())

	_, err := srv.CreateApp(context.Background(),
		&panelyv1.CreateAppRequest{Spec: testSpec()})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("kod = %v, beklenen AlreadyExists (%v)", status.Code(err), err)
	}
}

// TestCreateAppValidation, tanımın reddedildiği durumları tarar.
//
// Her durumda İKİ şey doğrulanıyor: kod InvalidArgument ve uygulama
// veritabanına YAZILMAMIŞ. İkincisi olmadan test "hata döndü"ye
// yaslanırdı ve reddedilen bir isteğin yan etki bırakıp bırakmadığını
// söylemezdi.
func TestCreateAppValidation(t *testing.T) {
	cases := []struct {
		name  string
		mutil func(*panelyv1.AppSpec)
	}{
		{"büyük harfli app_id", func(s *panelyv1.AppSpec) { s.AppId = "Blog" }},
		{"rakamla başlayan app_id", func(s *panelyv1.AppSpec) { s.AppId = "1blog" }},
		{"eğik çizgili app_id", func(s *panelyv1.AppSpec) { s.AppId = "a/b" }},
		{"boş app_id", func(s *panelyv1.AppSpec) { s.AppId = "" }},
		{"şemalı git_host", func(s *panelyv1.AppSpec) { s.GitHost = "https://github.com" }},
		{"portlu git_host", func(s *panelyv1.AppSpec) { s.GitHost = "github.com:443" }},
		{"eğik çizgili owner", func(s *panelyv1.AppSpec) { s.GitOwner = "a/b" }},
		{"nokta nokta repo", func(s *panelyv1.AppSpec) { s.GitRepo = ".." }},
		{"mutlak dockerfile", func(s *panelyv1.AppSpec) { s.DockerfilePath = "/etc/passwd" }},
		{"kaçan dockerfile", func(s *panelyv1.AppSpec) { s.DockerfilePath = "../Dockerfile" }},
		{"windows dockerfile", func(s *panelyv1.AppSpec) { s.DockerfilePath = `C:\evil` }},
		{"temiz olmayan dockerfile", func(s *panelyv1.AppSpec) { s.DockerfilePath = "./a/../Dockerfile" }},
		{"sıfır port", func(s *panelyv1.AppSpec) { s.ContainerPort = 0 }},
		{"sıfır replika", func(s *panelyv1.AppSpec) { s.Replicas = 0 }},
		{"aşırı replika", func(s *panelyv1.AppSpec) { s.Replicas = 1000 }},
		{"eğik çizgisiz health_path", func(s *panelyv1.AppSpec) { s.HealthPath = "healthz" }},
		{"satır sonlu health_path", func(s *panelyv1.AppSpec) { s.HealthPath = "/a\r\nHost: x" }},
		{"limitsiz", func(s *panelyv1.AppSpec) { s.Limits = nil }},
		{"sıfır bellek", func(s *panelyv1.AppSpec) { s.Limits.MemoryBytes = 0 }},
		{"sıfır cpu", func(s *panelyv1.AppSpec) { s.Limits.CpuMillis = 0 }},
		{"aralık dışı blkio", func(s *panelyv1.AppSpec) { s.Limits.BlkioWeight = 5 }},
		{"geçersiz derleme argümanı adı", func(s *panelyv1.AppSpec) {
			s.BuildArgs = map[string]string{"bad-name": "x"}
		}},
		{"NUL içeren derleme argümanı", func(s *panelyv1.AppSpec) {
			s.BuildArgs = map[string]string{"OK": "a\x00b"}
		}},
		{"şemalı domain", func(s *panelyv1.AppSpec) { s.Domain = "https://x.com" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, db := newDeployServer(t, &fakeExec{})
			spec := testSpec()
			tc.mutil(spec)

			_, err := srv.CreateApp(context.Background(),
				&panelyv1.CreateAppRequest{Spec: spec})
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("kod = %v, beklenen InvalidArgument (%v)", status.Code(err), err)
			}

			apps, err := db.ListApps(context.Background())
			if err != nil {
				t.Fatalf("uygulamalar okunamadı: %v", err)
			}
			if len(apps) != 0 {
				t.Errorf("reddedilen tanım yine de yazıldı: %v", apps)
			}
		})
	}
}

// TestRejectedCreateIsRecordedAsDenied, güvenlik modelinin devreye
// girdiği anın AYRICA izlenebilir olduğunu doğrular.
//
// DENIED ile FAILURE aynı kutuya konsaydı, "şema bir isteği reddetti"
// sinyali sıradan hataların gürültüsünde kaybolurdu.
func TestRejectedCreateIsRecordedAsDenied(t *testing.T) {
	srv, db := newDeployServer(t, &fakeExec{})

	spec := testSpec()
	spec.AppId = "Blog"
	_, _ = srv.CreateApp(context.Background(), &panelyv1.CreateAppRequest{Spec: spec})

	recs := auditActions(t, db)
	if len(recs) != 1 {
		t.Fatalf("%d denetim kaydı, 1 bekleniyordu", len(recs))
	}
	if recs[0].Action != "app.create" {
		t.Errorf("eylem = %q", recs[0].Action)
	}
	if recs[0].Outcome != audit.OutcomeDenied {
		t.Errorf("sonuç = %v, beklenen DENIED", recs[0].Outcome)
	}
}

// TestCreateAppAuditOmitsBuildArgValues, derleme argümanı DEĞERLERİNİN
// zincire girmediğini doğrular.
//
// Ölçüldü: bu değerler `docker history` çıktısında düz metin görünüyor,
// yani sır taşımamaları gerekiyor. "Gerekiyor" ile "öyle" aynı şey değil
// ve zincir ekle-sadece: bir kez giren geri alınamaz.
func TestCreateAppAuditOmitsBuildArgValues(t *testing.T) {
	srv, db := newDeployServer(t, &fakeExec{})

	spec := testSpec()
	spec.BuildArgs = map[string]string{"API_TOKEN": "cok-gizli-deger"}
	mustCreateApp(t, srv, spec)

	for _, rec := range auditActions(t, db) {
		if strings.Contains(rec.ParamsJSON, "cok-gizli-deger") {
			t.Fatalf("derleme argümanı DEĞERİ zincire yazıldı: %s", rec.ParamsJSON)
		}
		if !strings.Contains(rec.ParamsJSON, "API_TOKEN") {
			t.Errorf("derleme argümanı ADI kaydedilmedi: %s", rec.ParamsJSON)
		}
	}
}

func TestGetAppReturnsReleasesNewestFirst(t *testing.T) {
	srv, _ := newDeployServer(t, &fakeExec{})
	mustCreateApp(t, srv, testSpec())

	for range 3 {
		st := newDeployStream(context.Background())
		if err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st); err != nil {
			t.Fatalf("dağıtım başarısız: %v", err)
		}
	}

	resp, err := srv.GetApp(context.Background(), &panelyv1.GetAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	rels := resp.GetReleases()
	if len(rels) != 3 {
		t.Fatalf("%d sürüm döndü, 3 bekleniyordu", len(rels))
	}
	for i, want := range []string{"r3", "r2", "r1"} {
		if rels[i].GetReleaseId() != want {
			t.Errorf("%d. sürüm %q, beklenen %q", i, rels[i].GetReleaseId(), want)
		}
	}
	if resp.GetApp().GetReleaseCount() != 3 {
		t.Errorf("sürüm sayısı = %d", resp.GetApp().GetReleaseCount())
	}
}

func TestGetAppReportsMissingAsNotFound(t *testing.T) {
	srv, _ := newDeployServer(t, &fakeExec{})
	_, err := srv.GetApp(context.Background(), &panelyv1.GetAppRequest{AppId: "yok"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("kod = %v, beklenen NotFound (%v)", status.Code(err), err)
	}
}

func TestListAppsIsEmptyNotNil(t *testing.T) {
	srv, _ := newDeployServer(t, &fakeExec{})

	resp, err := srv.ListApps(context.Background(), &panelyv1.ListAppsRequest{})
	if err != nil {
		t.Fatalf("liste başarısız: %v", err)
	}
	if resp.GetApps() == nil {
		// Boş liste ile "alan yok" JSON'da farklı görünür; sidecar'ın
		// JSON-RPC çıktısını tüketen GUI için bu fark önemli.
		t.Error("boş liste nil döndü")
	}
}

// TestReadOnlyQueriesStayOutOfTheChain, salt okunur çağrıların zincire
// yazılmadığını doğrular.
//
// Her şeyi kaydeden bir günlük hiçbir şey kaydetmemeye yaklaşır: durum
// ekranının yenilenme gürültüsü, asıl önemli olan durum değiştiren
// işlemleri görünmez kılardı.
func TestReadOnlyQueriesStayOutOfTheChain(t *testing.T) {
	srv, db := newDeployServer(t, &fakeExec{})
	mustCreateApp(t, srv, testSpec())

	before := len(auditActions(t, db))

	for range 5 {
		if _, err := srv.ListApps(context.Background(), &panelyv1.ListAppsRequest{}); err != nil {
			t.Fatalf("liste başarısız: %v", err)
		}
		if _, err := srv.GetApp(context.Background(),
			&panelyv1.GetAppRequest{AppId: "blog"}); err != nil {
			t.Fatalf("okuma başarısız: %v", err)
		}
	}

	if after := len(auditActions(t, db)); after != before {
		t.Errorf("salt okunur çağrılar zincire %d kayıt ekledi", after-before)
	}
}

// TestNewServerRejectsTypedNilExecutor, arayüze konmuş TİPLİ bir nil'in
// kurucuda yakalandığını doğrular.
//
// Bu hatalı durum, Executor somut tipten arayüze çevrilince TEMSİL
// EDİLEBİLİR hale geldi. Düz `== nil` onu YAKALAMAZ: arayüzün tip sözcüğü
// dolu, yalnızca değeri nil. Kontrol olmasaydı panelyd hatalı kablolamayla
// ayağa kalkar ve ilk RPC'de nil başvurusuyla düşerdi — yani hata kurulum
// yerinden uzakta görünürdü.
func TestNewServerRejectsTypedNilExecutor(t *testing.T) {
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	defer func() { _ = db.Close() }()

	var typedNil *execclient.Client // nil işaretçi, arayüze konuyor

	// Tehlikenin gerçek olduğu ÇALIŞMA ZAMANINDA iddia edilmiyor, çünkü
	// gerek yok: staticcheck bunu STATİK olarak kanıtlıyor. Aynı değeri
	// bir Executor'a atayıp `== nil` ile karşılaştıran bir satır SA4023
	// veriyor — "this comparison is never true". Yani düz `== nil`
	// kontrolünün bu durumu asla yakalayamayacağı derleyici tarafından
	// doğrulanmış bir olgu.
	//
	// Testin ayırt ediciliği ayrıca mutasyonla sınandı: checkExecutor'daki
	// reflect kontrolü silindiğinde bu test KIRMIZIYA döndü.

	if _, err := NewServer(ServerOptions{Store: db, Executor: typedNil}); err == nil {
		t.Fatal("tipli nil executor kabul edildi — ilk RPC'de nil başvurusu olurdu")
	}
}

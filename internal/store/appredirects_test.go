package store

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/appnames"
)

func boolptr(b bool) *bool { return &b }

// namesApp, ad testleri için domain/www/açık adları ayarlı bir uygulama kurar.
func namesApp(id, domain string, www bool, redirects ...string) App {
	app := sampleApp(id)
	app.Domain = domain
	app.WWW = www
	app.Redirects = redirects
	return app
}

func TestCreateAppStoresWWWAndRedirects(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()

	created := mustCreate(t, s, namesApp("site", "example.com", true,
		"b.example.org", "a.example.org", "b.example.org"))

	want := []string{"a.example.org", "b.example.org"}
	if !reflect.DeepEqual(created.Redirects, want) {
		t.Errorf("CreateApp dönüşü Redirects = %v, %v bekleniyordu", created.Redirects, want)
	}
	got, err := s.GetApp(ctx, "site")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if !got.WWW {
		t.Error("WWW diskte kayboldu")
	}
	if !reflect.DeepEqual(got.Redirects, want) {
		t.Errorf("Redirects = %v, %v (sıralı, tekrarsız) bekleniyordu", got.Redirects, want)
	}

	plain := mustCreate(t, s, sampleApp("plain"))
	if plain.Redirects != nil {
		t.Errorf("adsız uygulamada Redirects = %#v, nil bekleniyordu", plain.Redirects)
	}
	read, _ := s.GetApp(ctx, "plain")
	if read.WWW || read.Redirects != nil {
		t.Errorf("adsız uygulama okundu: WWW=%v Redirects=%#v", read.WWW, read.Redirects)
	}
}

// TestMigrationLeavesExistingAppsWithoutWWW, göç 0010'un canlıdaki bir
// uygulamayı KAPALI bıraktığını ölçer (davranış göçle değişmemeli).
func TestMigrationLeavesExistingAppsWithoutWWW(t *testing.T) {
	ctx := context.Background()

	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)

	s := &Store{db: db}
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		) STRICT;`); err != nil {
		t.Fatalf("schema_migrations oluşturulamadı: %v", err)
	}
	for _, m := range []string{
		"0001_init.sql", "0002_apps.sql", "0003_deployments.sql",
		"0004_app_domain_unique.sql", "0005_deployment_history.sql",
		"0006_app_env.sql", "0007_app_volumes.sql", "0008_alarms.sql",
		"0009_env_seal.sql",
	} {
		if err := s.applyMigration(ctx, m); err != nil {
			t.Fatalf("%s uygulanamadı: %v", m, err)
		}
	}

	// Ham INSERT: CreateApp güncel sütunları yazar, 0009 şemasında yok.
	if _, err := db.ExecContext(ctx, `
		INSERT INTO apps (
			id, git_host, git_owner, git_repo, git_branch,
			dockerfile_path, build_args_json,
			container_port, replicas, health_path, domain,
			memory_bytes, cpu_millis, blkio_weight,
			release_seq, created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?, ?,?,?,?, ?,?,?, 0,?,?)`,
		"portfolio", "github.com", "erkanrzgc", "kadran", "main",
		"Dockerfile", "{}",
		8080, 2, "/healthz", "example.com",
		256<<20, 500, 500,
		1_700_000_000_000_000_000, 1_700_000_000_000_000_000,
	); err != nil {
		t.Fatalf("eski şemaya uygulama yazılamadı: %v", err)
	}

	if err := s.applyMigration(ctx, "0010_app_redirects.sql"); err != nil {
		t.Fatalf("0010 uygulanamadı: %v", err)
	}

	app, err := s.GetApp(ctx, "portfolio")
	if err != nil {
		t.Fatalf("göçten sonra uygulama okunamadı: %v", err)
	}
	if app.WWW {
		t.Error("mevcut uygulama göçle www AÇIK başladı")
	}
	if app.Redirects != nil {
		t.Errorf("Redirects = %#v, nil bekleniyordu", app.Redirects)
	}
	if got := appnames.Names(app.Domain, app.WWW, app.Redirects); len(got) != 0 {
		t.Errorf("göçten sonra türetilen adlar %v, boş bekleniyordu", got)
	}
}

func TestCreateAppRejectsNameUsedAsPartner(t *testing.T) {
	s := newAppStore(t)
	mustCreate(t, s, namesApp("site", "example.com", true))

	_, err := s.CreateApp(context.Background(), namesApp("blog", "www.example.com", false))
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("hata = %v, ErrNameTaken bekleniyordu", err)
	}
	if !strings.Contains(err.Error(), `"site"`) {
		t.Errorf("hata eşin sahibini adlandırmıyor: %v", err)
	}
	if _, gerr := s.GetApp(context.Background(), "blog"); !errors.Is(gerr, ErrAppNotFound) {
		t.Errorf("reddedilen uygulama yine de yazılmış: %v", gerr)
	}
}

func TestCreateAppRejectsRedirectThatIsAnotherDomain(t *testing.T) {
	s := newAppStore(t)
	mustCreate(t, s, namesApp("site", "example.com", false))

	_, err := s.CreateApp(context.Background(),
		namesApp("blog", "blog.example.org", false, "example.com"))
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("hata = %v, ErrNameTaken bekleniyordu", err)
	}
	if !strings.Contains(err.Error(), `"site"`) {
		t.Errorf("hata sahibi adlandırmıyor: %v", err)
	}
}

func TestCreateAppKeepsDomainTaken(t *testing.T) {
	s := newAppStore(t)
	mustCreate(t, s, namesApp("site", "example.com", false))

	_, err := s.CreateApp(context.Background(), namesApp("other", "example.com", false))
	if !errors.Is(err, ErrDomainTaken) && !errors.Is(err, ErrNameTaken) {
		t.Fatalf("hata = %v, alan adı çakışması bekleniyordu", err)
	}
}

func TestUpdateAppMovesPartnerWithDomain(t *testing.T) {
	s := newAppStore(t)
	mustCreate(t, s, namesApp("site", "example.com", true))

	got := mustUpdate(t, s, "site", AppUpdate{Domain: strptr("new.com")})
	names := appnames.Names(got.Domain, got.WWW, got.Redirects)
	if !reflect.DeepEqual(names, []string{"www.new.com"}) {
		t.Errorf("Names = %v, [www.new.com] bekleniyordu", names)
	}
}

func TestUpdateAppRejectsPartnerConflict(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()
	mustCreate(t, s, namesApp("site", "example.com", false))
	mustCreate(t, s, namesApp("blog", "blog.example.org", false, "www.example.com"))

	_, err := s.UpdateApp(ctx, "site", AppUpdate{WWW: boolptr(true)})
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("hata = %v, ErrNameTaken bekleniyordu", err)
	}
	if !strings.Contains(err.Error(), `"blog"`) {
		t.Errorf("hata sahibi adlandırmıyor: %v", err)
	}
	after, _ := s.GetApp(ctx, "site")
	if after.WWW {
		t.Error("reddedilen güncelleme kaydı DEĞİŞTİRDİ")
	}
}

func TestUpdateAppRedirectAddRemove(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()
	cur := mustCreate(t, s, namesApp("site", "example.com", false))

	upd := AppUpdate{RedirectAdd: []string{"a.com"}, RedirectRemove: []string{"b.com"}}
	if upd.IsEmpty() {
		t.Error("yönlendirme alanları IsEmpty'de sayılmıyor")
	}
	if !upd.ChangesNames(cur) {
		t.Error("ad ekleyen güncelleme ChangesNames=false")
	}
	got := mustUpdate(t, s, "site", upd)
	if !reflect.DeepEqual(got.Redirects, []string{"a.com"}) {
		t.Errorf("Redirects = %v, [a.com] bekleniyordu", got.Redirects)
	}

	// Olmayan adı çıkarmak sessiz ve ad kümesini değiştirmez.
	noop := AppUpdate{RedirectRemove: []string{"zzz.com"}}
	if noop.ChangesNames(got) {
		t.Error("olmayan adı çıkarmak ChangesNames=true döndü")
	}
	if _, err := s.UpdateApp(ctx, "site", noop); err != nil {
		t.Fatalf("olmayan adı çıkarmak hata verdi: %v", err)
	}
	if (AppUpdate{WWW: boolptr(false)}).ChangesNames(got) {
		t.Error("aynı www değeri ChangesNames=true döndü")
	}
	if !(AppUpdate{WWW: boolptr(true)}).ChangesNames(got) {
		t.Error("www açmak ChangesNames=false döndü")
	}

	// Çıkarınca liste boşalır ve nil'e döner.
	emptied := mustUpdate(t, s, "site", AppUpdate{RedirectRemove: []string{"a.com"}})
	if emptied.Redirects != nil {
		t.Errorf("boşalan Redirects = %#v, nil bekleniyordu", emptied.Redirects)
	}
}

func TestActiveDeploymentsCarryNames(t *testing.T) {
	s := newAppStore(t)
	ctx := context.Background()
	mustCreate(t, s, namesApp("site", "example.com", true, "old.example.org"))
	rel := buildRelease(t, s, "site", hexSHA(1))
	if err := s.SetActiveRelease(ctx, "site", rel.ID); err != nil {
		t.Fatalf("canlıya alınamadı: %v", err)
	}

	deps, err := s.ActiveDeployments(ctx)
	if err != nil {
		t.Fatalf("dağıtımlar okunamadı: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("%d dağıtım, 1 bekleniyordu", len(deps))
	}
	if !deps[0].WWW || !reflect.DeepEqual(deps[0].Redirects, []string{"old.example.org"}) {
		t.Errorf("dağıtım adları: WWW=%v Redirects=%v", deps[0].WWW, deps[0].Redirects)
	}
	one, err := s.ActiveDeployment(ctx, "site")
	if err != nil {
		t.Fatalf("tek dağıtım okunamadı: %v", err)
	}
	if !one.WWW || !reflect.DeepEqual(one.Redirects, []string{"old.example.org"}) {
		t.Errorf("tek dağıtım adları: WWW=%v Redirects=%v", one.WWW, one.Redirects)
	}
}

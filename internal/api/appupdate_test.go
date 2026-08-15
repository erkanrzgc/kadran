package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/deploy"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// fakeReconciler, uzlaştırmanın ÜÇ ayrı sonucunu taklit eder.
//
// Sınanan şey "çağrıldı mı" değil, KAÇ KEZ çağrıldığı ve sonucun
// kullanıcıya nasıl anlatıldığı. Yalnızca çağrılmayı saymak, alan adı
// değişmediği hâlde her güncellemede bütün siteleri yeniden yükleyen bir
// uygulamayı geçirirdi.
type fakeReconciler struct {
	calls  int
	result deploy.Result
	err    error
}

func (f *fakeReconciler) Reconcile(context.Context) (deploy.Result, error) {
	f.calls++
	if f.result.Skipped == nil {
		f.result.Skipped = map[string]string{}
	}
	return f.result, f.err
}

func newUpdateServer(t *testing.T, rec *fakeReconciler) (*Server, *store.Store) {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv, err := NewServer(ServerOptions{
		Store: db, Executor: &fakeExec{},
		Rollout: &fakeRollout{}, Reconciler: rec,
	})
	if err != nil {
		t.Fatalf("sunucu oluşturulamadı: %v", err)
	}
	return srv, db
}

func update(t *testing.T, srv *Server, req *panelyv1.UpdateAppRequest) *panelyv1.UpdateAppResponse {
	t.Helper()
	resp, err := srv.UpdateApp(context.Background(), req)
	if err != nil {
		t.Fatalf("güncelleme başarısız: %v", err)
	}
	return resp
}

func strp(s string) *string { return &s }
func u32p(v uint32) *uint32 { return &v }

// TestUpdateAppWritesOnlyTheNamedFields, kısmi güncellemenin uçtan uca
// çalıştığını sınar: iddia GetApp üzerinden, yani DİSKTEN yapılıyor.
func TestUpdateAppWritesOnlyTheNamedFields(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Domain = "eski.example.com"
	mustCreateApp(t, srv, spec)

	update(t, srv, &panelyv1.UpdateAppRequest{
		AppId:  "blog",
		Domain: strp("yeni.example.com"),
	})

	got := mustGetSpec(t, srv, "blog")
	if got.GetDomain() != "yeni.example.com" {
		t.Errorf("alan adı = %q", got.GetDomain())
	}
	if got.GetHealthPath() != "/healthz" {
		t.Errorf("health_path silindi: %q", got.GetHealthPath())
	}
	if got.GetGitBranch() != "main" {
		t.Errorf("git_branch silindi: %q", got.GetGitBranch())
	}
	if got.GetReplicas() != 1 {
		t.Errorf("replicas değişti: %d", got.GetReplicas())
	}
	if got.GetContainerPort() != 8080 {
		t.Errorf("container_port değişti: %d", got.GetContainerPort())
	}
}

// TestUpdateAppCanClearTheDomain, "belirtilmedi" ile "boşalt" ayrımının
// tel üzerinde de korunduğunu sınar. Bir üstteki testle birlikte anlamlı:
// tek başına o test, hiçbir şey yazmayan bir uygulamayı da geçirirdi.
func TestUpdateAppCanClearTheDomain(t *testing.T) {
	rec := &fakeReconciler{}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "eski.example.com"
	mustCreateApp(t, srv, spec)

	resp := update(t, srv, &panelyv1.UpdateAppRequest{
		AppId:  "blog",
		Domain: strp(""),
	})

	if got := mustGetSpec(t, srv, "blog").GetDomain(); got != "" {
		t.Errorf("alan adı temizlenmedi: %q", got)
	}
	// Alan adının KALKMASI da bir rota değişikliğidir: uzlaştırma
	// koşmazsa ters vekil silinmiş bir alan adına hizmet vermeye devam
	// eder.
	if rec.calls != 1 {
		t.Errorf("uzlaştırma %d kez koştu, 1 olmalıydı", rec.calls)
	}
	if !strings.Contains(resp.GetProxyDetail(), "ÇIKARILDI") {
		t.Errorf("vekil ayrıntısı uygulamanın çıkarıldığını söylemiyor: %q",
			resp.GetProxyDetail())
	}
}

// TestUpdateAppReconcilesOnlyWhenTheDomainActuallyChanges, gereksiz
// yeniden yüklemeyi engeller.
//
// Caddy'nin `POST /load` ucu kök nesnenin TAMAMINI değiştiriyor, yani her
// uzlaştırma sunucudaki BÜTÜN sitelerin yapılandırmasını yeniden yazıyor.
// Dala dokunan bir güncelleme yüzünden bunu yapmak gereksiz risk.
func TestUpdateAppReconcilesOnlyWhenTheDomainActuallyChanges(t *testing.T) {
	rec := &fakeReconciler{}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "sabit.example.com"
	mustCreateApp(t, srv, spec)

	// 1) Alan adına hiç dokunulmuyor.
	update(t, srv, &panelyv1.UpdateAppRequest{AppId: "blog", GitBranch: strp("develop")})
	if rec.calls != 0 {
		t.Errorf("dal değişikliği uzlaştırma tetikledi (%d kez)", rec.calls)
	}

	// 2) Alan adı belirtiliyor ama AYNI değerle.
	update(t, srv, &panelyv1.UpdateAppRequest{AppId: "blog", Domain: strp("sabit.example.com")})
	if rec.calls != 0 {
		t.Errorf("değişmeyen alan adı uzlaştırma tetikledi (%d kez)", rec.calls)
	}

	// 3) Alan adı GERÇEKTEN değişiyor.
	update(t, srv, &panelyv1.UpdateAppRequest{AppId: "blog", Domain: strp("baska.example.com")})
	if rec.calls != 1 {
		t.Fatalf("alan adı değişti ama uzlaştırma %d kez koştu, 1 olmalıydı", rec.calls)
	}
}

// TestUpdateAppSaysTheChangeWasSavedWhenTheProxyFails, en sinsi hatayı
// kapsar.
//
// Yazma BAŞARILI oldu, uzlaştırma başarısız. Düz bir "güncelleme
// başarısız" mesajı kullanıcıyı kaydın eski değerde kaldığını sanmaya
// iter — oysa yeni değerde. Bu, rollout.go'daki DrainError'ın aynı
// sınıfı: işlem oldu, arkasından gelen adım olmadı.
func TestUpdateAppSaysTheChangeWasSavedWhenTheProxyFails(t *testing.T) {
	rec := &fakeReconciler{err: errors.New("caddy admin soketi cevap vermiyor")}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "eski.example.com"
	mustCreateApp(t, srv, spec)

	_, err := srv.UpdateApp(context.Background(), &panelyv1.UpdateAppRequest{
		AppId: "blog", Domain: strp("yeni.example.com"),
	})
	if err == nil {
		t.Fatal("vekil hatası yutuldu")
	}
	if !strings.Contains(err.Error(), "KAYDEDİLDİ") {
		t.Errorf("hata, değişikliğin yazıldığını SÖYLEMİYOR: %v", err)
	}
	if !strings.Contains(err.Error(), "caddy admin soketi") {
		t.Errorf("hata asıl sebebi taşımıyor: %v", err)
	}

	// Ve gerçekten yazılmış olmalı — mesaj doğru ama durum yanlışsa
	// mesaj daha da kötü olurdu.
	if got := mustGetSpec(t, srv, "blog").GetDomain(); got != "yeni.example.com" {
		t.Errorf("hata 'kaydedildi' diyor ama diskte %q var", got)
	}
}

// TestUpdateAppWarnsWhenTrafficDidNotMove, uzlaştırmanın BAŞARILI olup
// bizim uygulamamızı atladığı durumu kapsar.
//
// Bu bir hata değil (hiç dağıtılmamış uygulamanın ayakta replikası
// olmaz), ama sessiz kalmak kullanıcıya yeni alan adının canlıda cevap
// verdiğini düşündürürdü — apex taşımasında tam olarak yanlış inanç.
func TestUpdateAppWarnsWhenTrafficDidNotMove(t *testing.T) {
	rec := &fakeReconciler{result: deploy.Result{
		Skipped: map[string]string{"blog": "aktif sürümün ayakta replikası yok"},
	}}
	srv, _ := newUpdateServer(t, rec)
	spec := testSpec()
	spec.Domain = "eski.example.com"
	mustCreateApp(t, srv, spec)

	resp := update(t, srv, &panelyv1.UpdateAppRequest{
		AppId: "blog", Domain: strp("yeni.example.com"),
	})

	detail := resp.GetProxyDetail()
	if !strings.Contains(detail, "TAŞINMADI") {
		t.Errorf("trafiğin taşınmadığı söylenmiyor: %q", detail)
	}
	if !strings.Contains(detail, "panely deploy blog") {
		t.Errorf("kullanıcıya ne yapacağı söylenmiyor: %q", detail)
	}
}

// TestUpdateAppValidatesTheMergedSpec, doğrulamanın delta üzerinde DEĞİL
// birleştirilmiş tanım üzerinde koştuğunu sınar.
func TestUpdateAppValidatesTheMergedSpec(t *testing.T) {
	cases := []struct {
		name string
		req  *panelyv1.UpdateAppRequest
	}{
		{"sıfır replika", &panelyv1.UpdateAppRequest{AppId: "blog", Replicas: u32p(0)}},
		{"aralık dışı replika", &panelyv1.UpdateAppRequest{AppId: "blog", Replicas: u32p(9999)}},
		{"şemalı alan adı", &panelyv1.UpdateAppRequest{AppId: "blog", Domain: strp("https://a.com")}},
		{"portlu alan adı", &panelyv1.UpdateAppRequest{AppId: "blog", Domain: strp("a.com:8080")}},
		{"eğik çizgisiz sağlık yolu", &panelyv1.UpdateAppRequest{AppId: "blog", HealthPath: strp("healthz")}},
		{"boşluklu sağlık yolu", &panelyv1.UpdateAppRequest{AppId: "blog", HealthPath: strp("/a b")}},
		{"geçersiz dal", &panelyv1.UpdateAppRequest{AppId: "blog", GitBranch: strp("-oProxyCommand=x")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &fakeReconciler{}
			srv, _ := newUpdateServer(t, rec)
			spec := testSpec()
			spec.Domain = "eski.example.com"
			mustCreateApp(t, srv, spec)

			_, err := srv.UpdateApp(context.Background(), tc.req)
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("kod = %v, beklenen InvalidArgument (%v)", status.Code(err), err)
			}
			// Reddedilen istek YAN ETKİ BIRAKMAMALI. "Hata döndü"ye
			// yaslanan bir test, yazdıktan sonra hata döndüren bir
			// uygulamayı da geçirirdi.
			if got := mustGetSpec(t, srv, "blog"); got.GetDomain() != "eski.example.com" ||
				got.GetReplicas() != 1 || got.GetHealthPath() != "/healthz" ||
				got.GetGitBranch() != "main" {
				t.Errorf("reddedilen istek yine de yazdı: %+v", got)
			}
			if rec.calls != 0 {
				t.Errorf("reddedilen istek ters vekile dokundu (%d kez)", rec.calls)
			}
		})
	}
}

func TestUpdateAppRejectsEmptyRequest(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})
	mustCreateApp(t, srv, testSpec())

	_, err := srv.UpdateApp(context.Background(), &panelyv1.UpdateAppRequest{AppId: "blog"})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("kod = %v, beklenen InvalidArgument (%v)", status.Code(err), err)
	}
}

func TestUpdateAppReportsMissingApp(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})

	_, err := srv.UpdateApp(context.Background(), &panelyv1.UpdateAppRequest{
		AppId: "yok", Domain: strp("a.example.com"),
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("kod = %v, beklenen NotFound (%v)", status.Code(err), err)
	}
}

// TestUpdateAppRejectsADomainOwnedByAnotherApp, göç 0004'ün kullanıcıya
// görünen yüzünü uçtan uca sınar.
func TestUpdateAppRejectsADomainOwnedByAnotherApp(t *testing.T) {
	srv, _ := newUpdateServer(t, &fakeReconciler{})

	first := testSpec()
	first.AppId = "portfolio"
	first.Domain = "panely.example.com"
	mustCreateApp(t, srv, first)

	second := testSpec()
	second.Domain = "blog.example.com"
	mustCreateApp(t, srv, second)

	_, err := srv.UpdateApp(context.Background(), &panelyv1.UpdateAppRequest{
		AppId: "blog", Domain: strp("panely.example.com"),
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("kod = %v, beklenen AlreadyExists (%v)", status.Code(err), err)
	}
	if !strings.Contains(err.Error(), "portfolio") {
		t.Errorf("hata çakışan uygulamayı adlandırmıyor: %v", err)
	}
}

// TestUpdateAppAuditRecordsOnlyTheNamedFields, denetim zincirinin
// dokunulmayan alanlar hakkında yalan söylemediğini sınar.
//
// Zincir ekle-sadece: "health_path boşaltıldı" diye yazılan bir kayıt,
// hiç dokunulmamış olsa bile sonsuza kadar orada kalır.
func TestUpdateAppAuditRecordsOnlyTheNamedFields(t *testing.T) {
	srv, db := newUpdateServer(t, &fakeReconciler{})
	spec := testSpec()
	spec.Domain = "eski.example.com"
	mustCreateApp(t, srv, spec)

	update(t, srv, &panelyv1.UpdateAppRequest{AppId: "blog", Domain: strp("yeni.example.com")})

	var rec *audit.Record
	for _, r := range auditActions(t, db) {
		if r.Action == "app.update" {
			rec = &r
			break
		}
	}
	if rec == nil {
		t.Fatal("app.update zincire hiç yazılmadı")
	}
	if rec.Outcome != audit.OutcomeSuccess {
		t.Errorf("sonuç = %v", rec.Outcome)
	}
	if rec.Target != "app/blog" {
		t.Errorf("hedef = %q", rec.Target)
	}
	if !strings.Contains(rec.ParamsJSON, "yeni.example.com") {
		t.Errorf("yeni alan adı kayda girmemiş: %s", rec.ParamsJSON)
	}
	for _, unwanted := range []string{"health_path", "replicas", "branch"} {
		if strings.Contains(rec.ParamsJSON, unwanted) {
			t.Errorf("dokunulmayan alan %q kayda girdi: %s", unwanted, rec.ParamsJSON)
		}
	}
}

func mustGetSpec(t *testing.T, srv *Server, appID string) *panelyv1.AppSpec {
	t.Helper()
	resp, err := srv.GetApp(context.Background(), &panelyv1.GetAppRequest{AppId: appID})
	if err != nil {
		t.Fatalf("uygulama okunamadı (%s): %v", appID, err)
	}
	return resp.GetApp().GetSpec()
}

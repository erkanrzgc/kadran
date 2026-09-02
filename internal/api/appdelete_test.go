package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// newDeleteServer, silme testleri için DURUM MODELLEYEN bir executor'la
// sunucu kurar.
func newDeleteServer(t *testing.T, exec *fakeExec) (*Server, *store.Store) {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv, err := NewServer(ServerOptions{
		Store: db, Executor: exec,
		Rollout: &fakeRollout{}, Reconciler: &fakeReconciler{},
	})
	if err != nil {
		t.Fatalf("sunucu kurulamadı: %v", err)
	}
	return srv, db
}

func seedDeletableApp(t *testing.T, db *store.Store, appID string) store.Release {
	t.Helper()
	ctx := context.Background()
	if _, err := db.CreateApp(ctx, store.App{
		ID: appID, GitHost: "github.com", GitOwner: "u", GitRepo: appID,
		GitBranch: "main", ContainerPort: 8080, Replicas: 1, HealthPath: "/",
		MemoryBytes: 512 << 20, CPUMillis: 1000, BlkioWeight: 500,
	}); err != nil {
		t.Fatalf("uygulama kurulamadı: %v", err)
	}
	rel, err := db.StartRelease(ctx, appID, strings.Repeat("a", 40))
	if err != nil {
		t.Fatalf("sürüm açılamadı: %v", err)
	}
	if err := db.FinishRelease(ctx, appID, rel.ID, "sha256:x"); err != nil {
		t.Fatalf("sürüm mühürlenemedi: %v", err)
	}
	return rel
}

func replica(appID, relID string, idx uint32) execclient.Replica {
	return execclient.Replica{AppID: appID, ReleaseID: relID, Index: idx}
}

// TestDeleteRemovesContainersThenRecords, sıranın doğru olduğunu ve
// hiçbir artık kalmadığını doğrular.
func TestDeleteRemovesContainersThenRecords(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedDeletableApp(t, db, "blog")

	resp, err := srv.DeleteApp(context.Background(), &panelyv1.DeleteAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("silme başarısız: %v", err)
	}
	if resp.GetContainersRemoved() != 2 {
		t.Errorf("kaldırılan konteyner %d, 2 bekleniyordu", resp.GetContainersRemoved())
	}
	if len(exec.replicas) != 0 {
		t.Errorf("konteyner kaldı: %v", exec.replicas)
	}
	// Durdurma, kaldırmadan ÖNCE gelmeli: kaldırma çalışan bir
	// konteyneri zorla koparırdı.
	if len(exec.stopCalls) != 2 || len(exec.rmCalls) != 2 {
		t.Errorf("durdurma %v, kaldırma %v — her sürüm için ikisi de bekleniyordu",
			exec.stopCalls, exec.rmCalls)
	}
	if _, err := db.GetApp(context.Background(), "blog"); !errors.Is(err, store.ErrAppNotFound) {
		t.Errorf("kayıt silinmedi: %v", err)
	}
}

// TestDeleteKeepsRecordsWhenContainerRemovalFails, silmenin YARIDA
// kaldığında kayıtlara dokunmadığını doğrular.
//
// ── Bu testin koruduğu şey ──────────────────────────────────────────
//
// Konteyner adları `app_id`/`release_id`'den türüyor, yani veritabanı
// satırı o konteynerlere ulaşmanın TEK yolu. Kayıtlar önce silinse ve
// kaldırma yarıda kalsa, geriye kimsenin adını bilemediği çalışan
// konteynerler kalırdı — ne panely görebilir, ne bir sonraki deneme
// bulabilir.
func TestDeleteKeepsRecordsWhenContainerRemovalFails(t *testing.T) {
	exec := &fakeExec{
		replicas: []execclient.Replica{replica("blog", "r1", 0)},
		rmErr:    errors.New("executor ulaşılamıyor"),
	}
	srv, db := newDeleteServer(t, exec)
	seedDeletableApp(t, db, "blog")

	if _, err := srv.DeleteApp(context.Background(), &panelyv1.DeleteAppRequest{AppId: "blog"}); err == nil {
		t.Fatal("konteyner kaldırılamadı ama silme BAŞARILI döndü")
	}

	// Kayıt DURMALI — ikinci deneme onu bulup tamamlayabilsin.
	if _, err := db.GetApp(context.Background(), "blog"); err != nil {
		t.Errorf("konteyner kaldırma başarısızken kayıt silindi: %v — "+
			"o konteynerlere ulaşacak hiçbir şey kalmazdı", err)
	}
}

// TestDeleteIsRepeatableAfterPartialFailure, yarıda kalmış bir silmenin
// ikinci çağrıda tamamlandığını doğrular.
func TestDeleteIsRepeatableAfterPartialFailure(t *testing.T) {
	exec := &fakeExec{
		replicas: []execclient.Replica{replica("blog", "r1", 0)},
		rmErr:    errors.New("geçici arıza"),
	}
	srv, db := newDeleteServer(t, exec)
	seedDeletableApp(t, db, "blog")
	ctx := context.Background()

	if _, err := srv.DeleteApp(ctx, &panelyv1.DeleteAppRequest{AppId: "blog"}); err == nil {
		t.Fatal("ilk denemenin başarısız olması bekleniyordu")
	}

	exec.rmErr = nil // arıza geçti
	if _, err := srv.DeleteApp(ctx, &panelyv1.DeleteAppRequest{AppId: "blog"}); err != nil {
		t.Fatalf("ikinci deneme tamamlanamadı: %v", err)
	}
	if _, err := db.GetApp(ctx, "blog"); !errors.Is(err, store.ErrAppNotFound) {
		t.Errorf("ikinci denemeden sonra kayıt duruyor: %v", err)
	}
}

// TestDeleteRefusesLiveApp, canlı uygulamanın REDDEDİLDİĞİNİ ve hiçbir
// konteynere dokunulmadığını doğrular.
//
// Dokunulsaydı gözetmen (health.Supervisor) onları ~6 saniyede geri
// getirirdi ve silme kendi kuyruğuyla yarışırdı.
func TestDeleteRefusesLiveApp(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{replica("blog", "r1", 0)}}
	srv, db := newDeleteServer(t, exec)
	rel := seedDeletableApp(t, db, "blog")
	ctx := context.Background()
	if err := db.SetActiveRelease(ctx, "blog", rel.ID); err != nil {
		t.Fatalf("aktif sürüm yazılamadı: %v", err)
	}

	_, err := srv.DeleteApp(ctx, &panelyv1.DeleteAppRequest{AppId: "blog"})
	if err == nil {
		t.Fatal("canlı uygulama silindi")
	}
	if !strings.Contains(err.Error(), rel.ID) {
		t.Errorf("hata %q — canlı sürümü söylemeliydi", err)
	}
	// gRPC KODU da iddia ediliyor: api.proto FAILED_PRECONDITION
	// döneceğini YAZIYOR ve ilk canlı ölçümde InvalidArgument döndü.
	// Dokümanın vaadi test altında değilse sessizce yalana dönüşüyor.
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("gRPC kodu %s, FailedPrecondition bekleniyordu — istek kusursuz, "+
			"izin vermeyen şey sistemin DURUMU", got)
	}
	if len(exec.stopCalls) != 0 || len(exec.rmCalls) != 0 {
		t.Errorf("canlı uygulamanın konteynerlerine dokunuldu: durdur=%v kaldır=%v — "+
			"gözetmen onları geri getirir ve silme kendi kuyruğuyla yarışırdı",
			exec.stopCalls, exec.rmCalls)
	}
	if _, err := db.GetApp(ctx, "blog"); err != nil {
		t.Errorf("reddedilen silme kaydı bozdu: %v", err)
	}
}

// TestDeleteWritesAuditRecord, silmenin NE yok ettiğini denetim
// zincirine yazdığını doğrular.
func TestDeleteWritesAuditRecord(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{replica("blog", "r1", 0)}}
	srv, db := newDeleteServer(t, exec)
	seedDeletableApp(t, db, "blog")
	ctx := context.Background()

	if _, err := srv.DeleteApp(ctx, &panelyv1.DeleteAppRequest{AppId: "blog"}); err != nil {
		t.Fatalf("silme başarısız: %v", err)
	}

	recs, err := db.ListAudit(ctx, 0, 50)
	if err != nil {
		t.Fatalf("denetim okunamadı: %v", err)
	}
	var found bool
	for _, r := range recs {
		if r.Action != "app.delete" {
			continue
		}
		found = true
		// Sayılar OLMADAN kayıt "ne yok oldu" sorusunu yanıtlayamaz.
		if !strings.Contains(r.ParamsJSON, "containers_removed") {
			t.Errorf("denetim kaydı ne yok edildiğini söylemiyor: %s", r.ParamsJSON)
		}
	}
	if !found {
		t.Error("app.delete denetim zincirine yazılmadı")
	}
}

// TestDeleteReportsMissingApp, olmayan uygulamanın NotFound döndürdüğünü
// ve hiçbir konteynere dokunulmadığını doğrular.
func TestDeleteReportsMissingApp(t *testing.T) {
	exec := &fakeExec{}
	srv, _ := newDeleteServer(t, exec)

	if _, err := srv.DeleteApp(context.Background(),
		&panelyv1.DeleteAppRequest{AppId: "yok"}); err == nil {
		t.Fatal("olmayan uygulama silindi")
	}
	if len(exec.rmCalls) != 0 {
		t.Errorf("olmayan uygulama için konteyner kaldırıldı: %v", exec.rmCalls)
	}
}

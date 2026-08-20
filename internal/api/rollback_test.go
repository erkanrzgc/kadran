package api

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// newRollbackServer, aktivasyonu GERÇEKTEN yazan bir sunucu kurar.
//
// fakeRollout varsayılan hâlinde depoya dokunmuyor; geri alma testleri
// dağıtım geçmişine dayandığı için burada bağlanıyor.
func newRollbackServer(t *testing.T, ro *fakeRollout) *Server {
	t.Helper()
	srv, db := newDeployServerWith(t, okBuild(), ro)
	ro.activations = db
	mustCreateApp(t, srv, testSpec())
	return srv
}

// deployN, uygulamayı n kez dağıtır ve sürüm kimliklerini döndürür.
//
// Gerçek Deploy akışından geçiyor: aktivasyon geçmişini yazan kod yolu o.
// Depoya doğrudan yazsaydık, handler'ın okuduğu geçmişi TESTİN kendisi
// üretirdi ve dağıtım akışının geçmişi doğru yazdığı hiç sınanmazdı.
func deployN(t *testing.T, srv *Server, n int) []string {
	t.Helper()

	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		stream := &deployStream{ctx: context.Background()}
		if err := srv.Deploy(&panelyv1.DeployRequest{
			AppId: "blog", CommitSha: apiSHA,
		}, stream); err != nil {
			t.Fatalf("%d. dağıtım başarısız: %v", i+1, err)
		}
		rel, err := srv.store.ActiveDeployment(context.Background(), "blog")
		if err != nil {
			t.Fatalf("%d. dağıtımdan sonra aktif sürüm okunamadı: %v", i+1, err)
		}
		ids = append(ids, rel.ReleaseID)
	}
	return ids
}

// TestRollbackTargetsThePreviouslyLiveRelease, handler'ın DOĞRU sürümü
// hedeflediğini doğrular.
//
// İddia "Rollback çağrıldı" değil, HANGİ sürümle çağrıldığı. Yanlış
// sürümü canlıya alan bir geri alma, varlık kontrolünden geçerdi ve
// operatör siteyi kurtardığını sanarken başka bir sürüme geçmiş olurdu.
func TestRollbackTargetsThePreviouslyLiveRelease(t *testing.T) {
	ro := &fakeRollout{}
	srv := newRollbackServer(t, ro)

	ids := deployN(t, srv, 3)
	prev, live := ids[1], ids[2]

	resp, err := srv.Rollback(context.Background(), &panelyv1.RollbackRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	if len(ro.rollbackCalls) != 1 || ro.rollbackCalls[0] != "blog/"+prev {
		t.Fatalf("rollback çağrıları %v, [blog/%s] bekleniyordu", ro.rollbackCalls, prev)
	}
	if len(ro.calls) != 3 {
		t.Errorf("geri alma DAĞITIM yolunu çağırdı (%v) — imaj yeniden derlenir", ro.calls)
	}

	// Yanıt trafiğin NEREDEN NEREYE taşındığını söylemeli: operatörün
	// doğrulaması gereken şey "hata almadım" değil.
	if resp.GetFromReleaseId() != live {
		t.Errorf("from = %q, %q bekleniyordu", resp.GetFromReleaseId(), live)
	}
	if resp.GetToReleaseId() != prev {
		t.Errorf("to = %q, %q bekleniyordu", resp.GetToReleaseId(), prev)
	}
}

// TestRollbackTwiceReturnsToTheNewerRelease, ARDIŞIK geri almaların
// aktivasyon geçmişini izlediğini doğrular.
//
// ── Ayırt edici senaryo ─────────────────────────────────────────────
//
//	r1, r2, r3 dağıtıldı        → canlı r3
//	geri al                     → canlı r2
//	geri al                     → canlı r3  ← BURASI
//
// Son adım "sürüm sırasında bir önceki" mantığıyla r1 derdi. Doğru cevap
// r3'tür: gerçekten canlı olan en son önceki sürüm odur. Tek bir geri alma
// sınayan test, yanlış uygulamayla da geçerdi.
func TestRollbackTwiceReturnsToTheNewerRelease(t *testing.T) {
	ro := &fakeRollout{}
	srv := newRollbackServer(t, ro)

	ids := deployN(t, srv, 3)
	r2, r3 := ids[1], ids[2]

	first, err := srv.Rollback(context.Background(), &panelyv1.RollbackRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("ilk geri alma başarısız: %v", err)
	}
	if first.GetToReleaseId() != r2 {
		t.Fatalf("ilk geri alma %q hedefledi, %q bekleniyordu", first.GetToReleaseId(), r2)
	}

	second, err := srv.Rollback(context.Background(), &panelyv1.RollbackRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("ikinci geri alma başarısız: %v", err)
	}
	if second.GetToReleaseId() != r3 {
		t.Fatalf("ikinci geri alma %q hedefledi, %q olmalıydı — "+
			"sürüm sırası aktivasyon geçmişinin yerine geçmiş",
			second.GetToReleaseId(), r3)
	}
}

// TestRollbackWithoutHistoryIsFailedPrecondition, geri alınacak bir şey
// olmadığında dönen kodun AYIRT EDİCİ olduğunu doğrular.
//
// NotFound dönseydi kullanıcı "böyle bir uygulama yok" diye okur ve yazım
// hatası aramaya başlardı; oysa uygulama var, eksik olan geçmişi.
func TestRollbackWithoutHistoryIsFailedPrecondition(t *testing.T) {
	ro := &fakeRollout{}
	srv := newRollbackServer(t, ro)

	// 1. Hiç dağıtılmamış.
	_, err := srv.Rollback(context.Background(), &panelyv1.RollbackRequest{AppId: "blog"})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("dağıtılmamış uygulamada kod %v, FailedPrecondition bekleniyordu", got)
	}

	// 2. Bir kez dağıtılmış — canlı var ama geri alınacak öncesi yok.
	deployN(t, srv, 1)
	_, err = srv.Rollback(context.Background(), &panelyv1.RollbackRequest{AppId: "blog"})
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("ilk dağıtımdan sonra kod %v, FailedPrecondition bekleniyordu", got)
	}
	if len(ro.rollbackCalls) != 0 {
		t.Errorf("geri alınacak sürüm yokken orkestratör çağrıldı: %v", ro.rollbackCalls)
	}

	// 3. Bilinmeyen uygulama AYRI kod almalı — kontrol grubu. Aynı koda
	//    düşselerdi test yalnızca "her şey FailedPrecondition" derdi.
	_, err = srv.Rollback(context.Background(), &panelyv1.RollbackRequest{AppId: "yok"})
	if got := status.Code(err); got != codes.NotFound {
		t.Errorf("bilinmeyen uygulamada kod %v, NotFound bekleniyordu", got)
	}
}

// TestRollbackRecordsWhatItDidInTheAuditChain, geri almanın denetim
// zincirine YAZILDIĞINI doğrular.
//
// Geri alma, canlı trafiği değiştiren geri alınamaz bir işlem; zincirde
// görünmezse "site ne zaman ve kimin eliyle değişti" sorusunun cevabı
// yoktur. Faz 1'in 6. kabul ölçütü bunu istiyor.
func TestRollbackRecordsWhatItDidInTheAuditChain(t *testing.T) {
	ro := &fakeRollout{}
	srv := newRollbackServer(t, ro)
	db := srv.store
	deployN(t, srv, 2)

	if _, err := srv.Rollback(
		context.Background(), &panelyv1.RollbackRequest{AppId: "blog"}); err != nil {
		t.Fatalf("geri alma başarısız: %v", err)
	}

	recs, err := db.ListAudit(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("denetim okunamadı: %v", err)
	}

	var found bool
	for _, r := range recs {
		if r.Action == "app.rollback" {
			found = true
		}
	}
	if !found {
		t.Fatal("geri alma denetim zincirinde YOK — canlıyı değiştiren " +
			"işlem iz bırakmadı")
	}

	// Zincir hâlâ geçerli olmalı: yeni kayıt onu bozmamalı.
	if _, err := db.VerifyAuditChain(context.Background()); err != nil {
		t.Errorf("denetim zinciri bozuldu: %v", err)
	}
}

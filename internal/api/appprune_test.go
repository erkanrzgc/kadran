package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// seedReleases, uygulamayı ve n adet MÜHÜRLENMİŞ sürümü kurar.
//
// Her sürümün commit'i farklı: şema 40 haneli hex zorluyor ve aynı
// commit'i tekrarlamak sürümleri ayırt edilemez kılardı.
func seedReleases(t *testing.T, db *store.Store, appID string, n int) []string {
	t.Helper()
	ctx := context.Background()
	if _, err := db.CreateApp(ctx, store.App{
		ID: appID, GitHost: "github.com", GitOwner: "u", GitRepo: appID,
		GitBranch: "main", ContainerPort: 8080, Replicas: 1, HealthPath: "/",
		MemoryBytes: 512 << 20, CPUMillis: 1000, BlkioWeight: 500,
	}); err != nil {
		t.Fatalf("uygulama kurulamadı: %v", err)
	}

	ids := make([]string, 0, n)
	for i := range n {
		sha := fmt.Sprintf("%040x", i+1)
		rel, err := db.StartRelease(ctx, appID, sha)
		if err != nil {
			t.Fatalf("sürüm açılamadı: %v", err)
		}
		if err := db.FinishRelease(ctx, appID, rel.ID, "sha256:x"); err != nil {
			t.Fatalf("sürüm mühürlenemedi: %v", err)
		}
		ids = append(ids, rel.ID)
	}
	return ids
}

// activate, sürümleri SIRAYLA canlıya alır — dağıtım geçmişi kurar.
func activate(t *testing.T, db *store.Store, appID string, releases ...string) {
	t.Helper()
	for _, id := range releases {
		if err := db.SetActiveRelease(context.Background(), appID, id); err != nil {
			t.Fatalf("sürüm %s aktifleştirilemedi: %v", id, err)
		}
	}
}

// ── Saklama politikası ───────────────────────────────────────────────

// TestPruneKeepsActiveAndRollbackTarget, iki sürümün korunduğunu
// doğrular.
//
// K-061 duran konteynerleri KASTEN biriktiriyor: geri alma duran bir
// konteyneri BAŞLATIYOR (saniyeler), imajdan kurmuyor (dakikalar).
// Budama geri alma hedefini silerse o gerekçe çöker.
func TestPruneKeepsActiveAndRollbackTarget(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0),
		replica("blog", "r3", 0), replica("blog", "r4", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 4)
	activate(t, db, "blog", "r1", "r2", "r3", "r4")

	resp, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("budama başarısız: %v", err)
	}

	if got := strings.Join(resp.GetPrunedReleases(), ","); got != "r1,r2" {
		t.Errorf("budanan sürümler %q, \"r1,r2\" bekleniyordu", got)
	}
	if got := strings.Join(resp.GetKeptReleases(), " | "); !strings.Contains(got, "r4 (aktif)") ||
		!strings.Contains(got, "r3 (geri alma hedefi)") {
		t.Errorf("korunanlar %q — r4 aktif, r3 geri alma hedefi olmalıydı", got)
	}
	if resp.GetContainersRemoved() != 2 {
		t.Errorf("%d konteyner kaldırıldı, 2 bekleniyordu", resp.GetContainersRemoved())
	}

	// ⚠ Sayıya güvenilmiyor: HANGİ sürümlere dokunulduğu okunuyor.
	// "2 kaldırıldı" iddiası, yanlış ikisi kaldırılsa da geçerdi.
	if got := strings.Join(exec.rmCalls, ","); got != "blog/r1,blog/r2" {
		t.Errorf("kaldırma çağrıları %q — yanlış sürümlere dokunuldu", got)
	}

	// Durdurma da AYRICA sınanıyor. Sürücünün ContainerRemove'u
	// `force=true` ile koşuyor, yani tek başına SIGKILL. Boşaltması
	// yarıda kalmış (hâlâ çalışan) bir eski sürüm, durdurma adımı
	// olmadan koparılırdı.
	if got := strings.Join(exec.stopCalls, ","); got != "blog/r1,blog/r2" {
		t.Errorf("durdurma çağrıları %q — kaldırma force=true ile koşuyor, "+
			"durdurma atlanırsa çalışan konteyner koparılır", got)
	}
}

// TestPruneAfterRollbackKeepsTheRightTarget, saklama kümesinin dağıtım
// GEÇMİŞİNDEN okunduğunu doğrular.
//
// ── Bu testin koruduğu şey ──────────────────────────────────────────
//
// Sürüm sırası aktivasyon geçmişi DEĞİLDİR ve göç 0005 tam olarak bunun
// için yazıldı. Senaryo: r1 → r2 → r3 dağıtıldı, sonra r1'e geri
// alındı. Aktif sürüm r1, ama bir sonraki geri alma hedefi r3'tür —
// çünkü gerçekten canlı olan en son önceki sürüm odur.
//
// `releases.seq` kullanan bir uygulama burada r1'in ÖNCESİNİ arar,
// bulamaz, ve r3'ü budar. Yani geri alma hedefini yok eder. Test tam
// olarak o hatayı ölçüyor: r3 korunmalı, r2 budanmalı.
func TestPruneAfterRollbackKeepsTheRightTarget(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0), replica("blog", "r3", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 3)
	// r1 → r2 → r3, sonra GERİ ALMA: r1 yeniden canlıya alınıyor.
	activate(t, db, "blog", "r1", "r2", "r3", "r1")

	resp, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("budama başarısız: %v", err)
	}

	kept := strings.Join(resp.GetKeptReleases(), " | ")
	if !strings.Contains(kept, "r1 (aktif)") {
		t.Errorf("korunanlar %q — r1 aktif olmalıydı", kept)
	}
	if !strings.Contains(kept, "r3 (geri alma hedefi)") {
		t.Errorf("korunanlar %q — geri alma hedefi r3 olmalıydı. "+
			"r2 görünüyorsa saklama kümesi releases.seq'ten okunuyor "+
			"demektir; sıra numarası aktivasyon geçmişi DEĞİLDİR (göç 0005)",
			kept)
	}
	if got := strings.Join(exec.rmCalls, ","); got != "blog/r2" {
		t.Errorf("kaldırma çağrıları %q, yalnızca blog/r2 bekleniyordu", got)
	}
}

// TestPruneFirstDeployKeepsOnlyActive, geri alınacak sürüm YOKKEN
// budamanın normal çalıştığını doğrular.
//
// `ErrNoPreviousDeployment` burada bir HATA değil, normal bir dal: ilk
// dağıtımda geri alınacak bir şey yoktur. Hata sayılsaydı, yeni
// dağıtılmış her uygulamada budama reddedilirdi.
func TestPruneFirstDeployKeepsOnlyActive(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0),
		// Kayıtlarda karşılığı olmayan bir artık: göç sırasında kalmış
		// ya da elle yaratılmış olabilir. Budama onu da temizlemeli.
		replica("blog", "r0", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 1)
	activate(t, db, "blog", "r1")

	resp, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("ilk dağıtımda budama reddedildi: %v", err)
	}
	if len(resp.GetKeptReleases()) != 1 {
		t.Errorf("korunan %v — yalnızca aktif sürüm beklenirdi",
			resp.GetKeptReleases())
	}
	if got := strings.Join(exec.rmCalls, ","); got != "blog/r0" {
		t.Errorf("kaldırma çağrıları %q, yalnızca blog/r0 bekleniyordu", got)
	}
}

// ── Fail-closed ──────────────────────────────────────────────────────

// TestPruneRefusesWhenNoActiveDeployment, aktif sürüm bilinemiyorsa
// HİÇBİR ŞEYE dokunulmadığını doğrular.
//
// ── Bu testin koruduğu şey ──────────────────────────────────────────
//
// "Korunacak sürüm yok, öyleyse hepsini sil" sonucu, `app delete`'in
// yıkıcılığını kapısız bir komuta kaçırırdı: hiç dağıtılmamış (ya da
// kaydı okunamayan) bir uygulamanın BÜTÜN konteynerleri silinirdi.
//
// ⚠ Yalnızca hata dönmesi yetmez — hata dönmeden ÖNCE silmiş olabilirdi.
// Bu yüzden executor çağrıları da sayılıyor.
func TestPruneRefusesWhenNoActiveDeployment(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 2) // dağıtım YOK

	_, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog"})
	if err == nil {
		t.Fatal("aktif sürümü olmayan uygulama budandı — fail-closed çalışmıyor")
	}
	if got := status.Code(err); got != codes.FailedPrecondition {
		t.Errorf("hata kodu %v, FailedPrecondition bekleniyordu — istek "+
			"kusursuz, izin vermeyen şey sistemin DURUMU", got)
	}
	if len(exec.stopCalls) != 0 || len(exec.rmCalls) != 0 {
		t.Errorf("reddedildiği hâlde executor çağrıldı: durdur=%v kaldır=%v",
			exec.stopCalls, exec.rmCalls)
	}
}

// TestPruneRejectsEmptyAppID, boş kimliğin "hepsi" ANLAMINA GELMEDİĞİNİ
// doğrular.
//
// proto3'te string presence taşımaz: alanı doldurmayı unutan çağıran ile
// boş gönderen çağıran telde ayırt edilemez (K-081'in aynı kökü). Boşu
// "bütün uygulamalar" saysaydık, bir unutma YIKICI bir işlemi her
// uygulamaya uygulatırdı.
func TestPruneRejectsEmptyAppID(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{replica("blog", "r1", 0)}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 1)
	activate(t, db, "blog", "r1")

	_, err := srv.PruneApp(context.Background(), &panelyv1.PruneAppRequest{})
	if err == nil {
		t.Fatal("boş app_id kabul edildi — bir unutma her uygulamayı budardı")
	}
	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("hata kodu %v, InvalidArgument bekleniyordu", got)
	}
	if len(exec.rmCalls) != 0 {
		t.Errorf("boş kimlikle executor çağrıldı: %v", exec.rmCalls)
	}
}

// ── Kontrol grubu: YABANCI konteynerlere dokunulmuyor ────────────────

// TestPruneNeverTouchesForeignContainers, budamanın yalnızca hedef
// uygulamaya dokunduğunu doğrular.
//
// ── Neden sayı saymak YETMEZ ────────────────────────────────────────
//
// "2 konteyner kaldırıldı" iddiası, kaldırılanlardan biri başka bir
// uygulamanınki olsa da geçerdi. Bu yüzden test, hostta duran BAŞKA
// uygulamaları adlarıyla arıyor: kaldırma çağrılarının hiçbirinde
// geçmemeliler.
//
// ⚠ Bu katmanın sınırı: gerçek etiket süzmesi sürücüde
// (`ContainerList` `panely.app_id` etiketiyle) ve onun kendi testleri
// var. Burada sınanan şey, budamanın executor'a DOĞRU app_id'yi
// geçirmesi ve dönen listenin dışına çıkmaması.
func TestPruneNeverTouchesForeignContainers(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0), replica("blog", "r3", 0),
		// Panely'nin başka uygulamaları ve Panely'ye AİT OLMAYAN işler.
		replica("dukkan", "r1", 0),
		replica("elastic-poincare", "r1", 0),
		replica("strange-nash", "r1", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 3)
	activate(t, db, "blog", "r1", "r2", "r3")

	if _, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog"}); err != nil {
		t.Fatalf("budama başarısız: %v", err)
	}

	touched := strings.Join(append(exec.stopCalls, exec.rmCalls...), " ")
	for _, foreign := range []string{"dukkan", "elastic-poincare", "strange-nash"} {
		if strings.Contains(touched, foreign) {
			t.Errorf("YABANCI konteynere dokunuldu (%s): %s", foreign, touched)
		}
	}
	// Kontrol grubunun kendisi ölçüyor mu: hedef uygulamaya GERÇEKTEN
	// dokunulmuş olmalı, yoksa "hiçbir yabancıya dokunulmadı" iddiası
	// hiçbir şey yapmayan bir budamada da geçerdi.
	if !strings.Contains(touched, "blog/r1") {
		t.Errorf("hedef uygulamaya hiç dokunulmamış (%s) — yukarıdaki "+
			"kontrol grubu iddiası ölçüm DEĞİL", touched)
	}
}

// ── Deneme modu ──────────────────────────────────────────────────────

// TestPruneDryRunRemovesNothing, denemenin HİÇBİR ŞEY silmediğini ama
// aynı listeyi ürettiğini doğrular.
func TestPruneDryRunRemovesNothing(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0), replica("blog", "r3", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 3)
	activate(t, db, "blog", "r1", "r2", "r3")

	resp, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog", DryRun: true})
	if err != nil {
		t.Fatalf("deneme başarısız: %v", err)
	}

	// Liste GERÇEK budamayla aynı olmalı: farklıysa deneme, kararı
	// dayandığı şeyi göstermiyor demektir.
	if got := strings.Join(resp.GetPrunedReleases(), ","); got != "r1" {
		t.Errorf("deneme listesi %q, \"r1\" bekleniyordu", got)
	}
	if resp.GetContainersRemoved() != 0 {
		t.Errorf("denemede %d konteyner sayıldı — 0 olmalı",
			resp.GetContainersRemoved())
	}
	if len(exec.stopCalls) != 0 || len(exec.rmCalls) != 0 {
		t.Errorf("DENEME modunda executor çağrıldı: durdur=%v kaldır=%v",
			exec.stopCalls, exec.rmCalls)
	}
}

// ── Ne yapılmadığı ───────────────────────────────────────────────────

// TestPruneSaysWhatItDidNotDo, yanıtın kapsam dışını BİLDİRDİĞİNİ
// doğrular (K-088).
//
// Kullanıcı "budadım" deyince diskin toparlandığını varsayar. Oysa
// budanan yalnızca konteyner: imajlar yerinde duruyor ve silinmiş
// uygulamaların yetim konteynerleri bu RPC'nin göremediği yerde.
func TestPruneSaysWhatItDidNotDo(t *testing.T) {
	exec := &fakeExec{replicas: []execclient.Replica{
		replica("blog", "r1", 0), replica("blog", "r2", 0),
	}}
	srv, db := newDeleteServer(t, exec)
	seedReleases(t, db, "blog", 2)
	activate(t, db, "blog", "r1", "r2")

	resp, err := srv.PruneApp(context.Background(),
		&panelyv1.PruneAppRequest{AppId: "blog"})
	if err != nil {
		t.Fatalf("budama başarısız: %v", err)
	}
	if !resp.GetImagesUntouched() {
		t.Error("images_untouched false — budama imaj silmiyor, " +
			"kullanıcı sildiğini sanır ve disk sessizce dolmaya devam eder")
	}
	if !resp.GetOrphansOutOfScope() {
		t.Error("orphans_out_of_scope false — uygulama başına budama " +
			"silinmiş uygulamaların konteynerlerini GÖREMEZ")
	}
}

// ── Beklenmedik veritabanı hatası ────────────────────────────────────

// stubDeployments, saklama kümesinin BAŞARISIZLIK yolunu sınamak için
// dağıtım sorgularını taklit eder.
type stubDeployments struct {
	active  store.Deployment
	prevErr error
}

func (s stubDeployments) ActiveDeployment(
	_ context.Context, _ string,
) (store.Deployment, error) {
	return s.active, nil
}

func (s stubDeployments) PreviousActiveRelease(
	_ context.Context, _ string,
) (string, error) {
	return "", s.prevErr
}

// TestKeepSetFailsOnUnexpectedDeploymentError, BEKLENMEDİK bir okuma
// hatasının yutulmadığını doğrular.
//
// ── Bu testin var olma sebebi bir MUTASYON ──────────────────────────
//
// İlk hâlde bu dal testte HİÇ uyarılmıyordu: sahte depo yalnızca başarı
// ya da `ErrNoPreviousDeployment` döndürüyordu. Hatayı sessizce yutan
// mutasyon sekiz testin sekizini de YEŞİL geçti.
//
// Yutulduğunda ne olurdu: geçici bir okuma hatasında saklama kümesi
// {aktif} olarak kalır, geri alma hedefi "eski sürüm" sayılır ve
// SİLİNİR. Sonraki `panely rollback` imajdan kurmak zorunda kalır —
// yani K-061'in bütün gerekçesi, bir yutulmuş hata yüzünden çöker.
//
// `ErrNoPreviousDeployment` ile BEKLENMEDİK hata aynı kola konulamaz:
// birincisi normal (ilk dağıtım), ikincisi sistemin durumu hakkında
// hiçbir şey bilmediğimiz anlamına geliyor. Fail-closed olan ikincisi.
func TestKeepSetFailsOnUnexpectedDeploymentError(t *testing.T) {
	boom := errors.New("veritabanı okunamadı: disk hatası")
	d := stubDeployments{
		active:  store.Deployment{AppID: "blog", ReleaseID: "r9"},
		prevErr: boom,
	}

	_, err := keepSet(context.Background(), d, "blog")
	if err == nil {
		t.Fatal("beklenmedik okuma hatası YUTULDU — budama eksik bir " +
			"saklama kümesiyle devam eder ve geri alma hedefini siler")
	}
	if !errors.Is(err, boom) {
		t.Errorf("hata %v — özgün sebep sarmalanmadan kaybolmuş", err)
	}

	// Kontrol grubu: AYNI koddan geçen normal dal hâlâ çalışıyor mu?
	// Olmasaydı "her şeye hata döndüren" bir uygulama da yukarıdaki
	// iddiayı geçerdi.
	keep, err := keepSet(context.Background(), stubDeployments{
		active:  store.Deployment{AppID: "blog", ReleaseID: "r9"},
		prevErr: store.ErrNoPreviousDeployment,
	}, "blog")
	if err != nil {
		t.Fatalf("ilk dağıtım dalı da hata döndü: %v — kontrol grubu "+
			"çöktü, yukarıdaki iddia ölçüm DEĞİL", err)
	}
	if len(keep) != 1 || keep["r9"] != "aktif" {
		t.Errorf("saklama kümesi %v, {r9: aktif} bekleniyordu", keep)
	}
}

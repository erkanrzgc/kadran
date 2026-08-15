package api

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// ── Test altyapısı ───────────────────────────────────────────────────

// fakeExec, Executor arayüzünü sınanabilir biçimde karşılar.
//
// Yalnızca ImageBuild anlamlı; diğerleri bu testlerde çağrılmıyor ve
// çağrılırlarsa hata dönerek KENDİLERİNİ duyururlar. Sessizce sıfır değer
// dönmek, testin farkında olmadığı bir kod yolunu gizlerdi.
type fakeExec struct {
	build      func(ctx context.Context, req *panelyv1.ImageBuildRequest, sink execclient.BuildSink) (string, error)
	buildCalls int
	lastReq    *panelyv1.ImageBuildRequest
}

func (f *fakeExec) Ping(context.Context) (execclient.PingResult, error) {
	return execclient.PingResult{}, errors.New("fakeExec: Ping beklenmiyordu")
}

func (f *fakeExec) HostInfo(context.Context) (*panelyv1.HostInfo, error) {
	return nil, errors.New("fakeExec: HostInfo beklenmiyordu")
}

func (f *fakeExec) ReadJournal(context.Context, uint64, uint32) (execclient.JournalPage, error) {
	return execclient.JournalPage{}, errors.New("fakeExec: ReadJournal beklenmiyordu")
}

func (f *fakeExec) ImageBuild(
	ctx context.Context, req *panelyv1.ImageBuildRequest, sink execclient.BuildSink,
) (string, error) {
	f.buildCalls++
	f.lastReq = req
	if f.build == nil {
		return "sha256:varsayilan", nil
	}
	return f.build(ctx, req, sink)
}

// deployStream, ServerStreamingServer'ı taklit eder.
//
// grpc.ServerStream KASTEN nil bırakılıyor: bu testlerin kullanmadığı bir
// metot çağrılırsa sessizce sıfır değer dönmek yerine GÜRÜLTÜLÜ biçimde
// panikler. Sınanmayan bir yolun sınanmış gibi görünmesi daha kötüdür.
type deployStream struct {
	grpc.ServerStream
	ctx      context.Context
	sent     []*panelyv1.DeployResponse
	failFrom int // bu indisten itibaren Send hata döner; 0 = hiç
	sendErr  error
}

func newDeployStream(ctx context.Context) *deployStream {
	return &deployStream{ctx: ctx, failFrom: -1}
}

func (s *deployStream) Context() context.Context     { return s.ctx }
func (s *deployStream) SetHeader(metadata.MD) error  { return nil }
func (s *deployStream) SendHeader(metadata.MD) error { return nil }
func (s *deployStream) SetTrailer(metadata.MD)       {}

func (s *deployStream) Send(m *panelyv1.DeployResponse) error {
	if s.failFrom >= 0 && len(s.sent) >= s.failFrom {
		return s.sendErr
	}
	s.sent = append(s.sent, m)
	return nil
}

func (s *deployStream) succeeded() *panelyv1.DeploySucceeded {
	for _, m := range s.sent {
		if v := m.GetSucceeded(); v != nil {
			return v
		}
	}
	return nil
}

func (s *deployStream) outputs() string {
	var b strings.Builder
	for _, m := range s.sent {
		if o := m.GetOutput(); o != nil {
			b.Write(o.GetData())
		}
	}
	return b.String()
}

func newDeployServer(t *testing.T, fe *fakeExec) (*Server, *store.Store) {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv, err := NewServer(ServerOptions{
		Store: db, Executor: fe,
		Rollout: &fakeRollout{}, Reconciler: &fakeReconciler{},
	})
	if err != nil {
		t.Fatalf("sunucu oluşturulamadı: %v", err)
	}
	return srv, db
}

func testSpec() *panelyv1.AppSpec {
	return &panelyv1.AppSpec{
		AppId:          "blog",
		GitHost:        "github.com",
		GitOwner:       "erkanrzgc",
		GitRepo:        "panely",
		GitBranch:      "main",
		DockerfilePath: "Dockerfile",
		ContainerPort:  8080,
		Replicas:       1,
		HealthPath:     "/healthz",
		Limits: &panelyv1.ResourceLimits{
			MemoryBytes: 256 << 20,
			CpuMillis:   500,
			BlkioWeight: 500,
		},
	}
}

const apiSHA = "0123456789abcdef0123456789abcdef01234567"

func mustCreateApp(t *testing.T, srv *Server, spec *panelyv1.AppSpec) {
	t.Helper()
	if _, err := srv.CreateApp(context.Background(),
		&panelyv1.CreateAppRequest{Spec: spec}); err != nil {
		t.Fatalf("uygulama yaratılamadı: %v", err)
	}
}

func auditActions(t *testing.T, db *store.Store) []audit.Record {
	t.Helper()
	recs, err := db.ListAudit(context.Background(), 0, 100)
	if err != nil {
		t.Fatalf("denetim okunamadı: %v", err)
	}
	return recs
}

// ── Testler ──────────────────────────────────────────────────────────

func TestDeployStreamsOutputAndSucceeds(t *testing.T) {
	fe := &fakeExec{
		build: func(_ context.Context, _ *panelyv1.ImageBuildRequest, sink execclient.BuildSink) (string, error) {
			if err := sink([]byte("Step 1/2\n"), false); err != nil {
				return "", err
			}
			if err := sink([]byte("Step 2/2\n"), false); err != nil {
				return "", err
			}
			return "sha256:abc123", nil
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st)
	if err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	// İlk mesaj sürümü bildirmeli: istemci koparsa bile adresleyebilsin.
	if acc := st.sent[0].GetAccepted(); acc == nil {
		t.Fatalf("ilk mesaj DeployAccepted değil: %v", st.sent[0])
	} else if acc.GetReleaseId() != "r1" {
		t.Errorf("sürüm kimliği %q", acc.GetReleaseId())
	}

	if got := st.outputs(); got != "Step 1/2\nStep 2/2\n" {
		t.Errorf("derleme çıktısı = %q", got)
	}

	// Başarının POZİTİF kanıtı: son mesaj DeploySucceeded ve imaj kimliği dolu.
	ok := st.succeeded()
	if ok == nil {
		t.Fatal("DeploySucceeded gönderilmedi — istemci başarıyı doğrulayamaz")
	}
	if ok.GetImageId() != "sha256:abc123" {
		t.Errorf("imaj kimliği %q", ok.GetImageId())
	}
	if last := st.sent[len(st.sent)-1]; last.GetSucceeded() == nil {
		t.Error("DeploySucceeded SON mesaj değil")
	}

	rel, err := db.GetRelease(context.Background(), "blog", "r1")
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if rel.Status != store.ReleaseBuilt || rel.ImageID != "sha256:abc123" {
		t.Errorf("sürüm mühürlenmedi: status=%d image=%q", rel.Status, rel.ImageID)
	}
}

// TestDeployBuildsTheRequestedCommit, executor'a giden isteğin uygulama
// tanımından ve İSTENEN sha'dan kurulduğunu doğrular.
//
// Kaynak üçlüsünün istekten DEĞİL veritabanından geldiği burada görünür:
// istemci yalnızca app_id + sha gönderiyor.
func TestDeployBuildsTheRequestedCommit(t *testing.T) {
	fe := &fakeExec{}
	srv, _ := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	if err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	src := fe.lastReq.GetSource()
	if src.GetHost() != "github.com" || src.GetOwner() != "erkanrzgc" || src.GetRepo() != "panely" {
		t.Errorf("kaynak üçlüsü uygulama tanımından gelmedi: %v", src)
	}
	if src.GetCommitSha() != apiSHA {
		t.Errorf("commit_sha = %q, beklenen %q", src.GetCommitSha(), apiSHA)
	}
	if ref := fe.lastReq.GetRelease(); ref.GetAppId() != "blog" || ref.GetReleaseId() != "r1" {
		t.Errorf("sürüm referansı yanlış: %v", ref)
	}
}

// TestDeployRejectsShortSHABeforeTouchingAnything, doğrulamanın HER ŞEYDEN
// ÖNCE geldiğini doğrular.
//
// İddia davranışsal: yalnızca "hata döndü" değil, executor'a HİÇ çağrı
// gitmemeli ve veritabanında sürüm satırı OLUŞMAMALI. "Hata döndü" tek
// başına zayıf bir iddiadır — hata başka bir sebepten de gelebilir.
func TestDeployRejectsShortSHABeforeTouchingAnything(t *testing.T) {
	fe := &fakeExec{}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: "abc123"}, st)

	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("kod = %v, beklenen InvalidArgument (%v)", status.Code(err), err)
	}
	if fe.buildCalls != 0 {
		t.Errorf("doğrulama reddine rağmen executor çağrıldı (%d kez)", fe.buildCalls)
	}
	if len(st.sent) != 0 {
		t.Errorf("reddedilen istekte istemciye mesaj gitti: %v", st.sent)
	}

	app, err := db.GetApp(context.Background(), "blog")
	if err != nil {
		t.Fatalf("uygulama okunamadı: %v", err)
	}
	if app.ReleaseSeq != 0 {
		t.Errorf("reddedilen istek sürüm sayacını artırdı: %d", app.ReleaseSeq)
	}
}

func TestDeployReportsUnknownAppAsNotFound(t *testing.T) {
	fe := &fakeExec{}
	srv, _ := newDeployServer(t, fe)

	st := newDeployStream(context.Background())
	err := srv.Deploy(&panelyv1.DeployRequest{AppId: "yok", CommitSha: apiSHA}, st)

	// NotFound, InvalidArgument DEĞİL: "yazım hatası yaptım" ile "bu
	// uygulama yok" istemci için ayırt edilebilir kalmalı.
	if status.Code(err) != codes.NotFound {
		t.Fatalf("kod = %v, beklenen NotFound (%v)", status.Code(err), err)
	}
	if fe.buildCalls != 0 {
		t.Errorf("bilinmeyen uygulamada executor çağrıldı")
	}
}

// TestFailedBuildSealsReleaseAndSendsNoSuccess, derleme ortada ölünce
// ne olduğunu doğrular.
func TestFailedBuildSealsReleaseAndSendsNoSuccess(t *testing.T) {
	buildErr := errors.New("docker: derleme başarısız: npm ERR! kayıp bağımlılık")
	fe := &fakeExec{
		build: func(_ context.Context, _ *panelyv1.ImageBuildRequest, sink execclient.BuildSink) (string, error) {
			_ = sink([]byte("Step 1/2\n"), false)
			return "", buildErr
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st)
	if err == nil {
		t.Fatal("başarısız derleme hata döndürmedi")
	}
	if st.succeeded() != nil {
		t.Fatal("başarısız derlemede DeploySucceeded gönderildi")
	}
	// Çıktı yine de akmış olmalı: kullanıcı hatayı görebilmeli.
	if !strings.Contains(st.outputs(), "Step 1/2") {
		t.Errorf("hata öncesi çıktı istemciye ulaşmadı: %q", st.outputs())
	}

	rel, err := db.GetRelease(context.Background(), "blog", "r1")
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if rel.Status != store.ReleaseFailed {
		t.Errorf("sürüm FAILED değil: %d", rel.Status)
	}
	if rel.ImageID != "" {
		t.Errorf("başarısız sürümde imaj kimliği var: %q", rel.ImageID)
	}
	// Sebep sürüm satırında SAKLANIR (değiştirilebilir tablo)…
	if !strings.Contains(rel.Detail, "npm ERR!") {
		t.Errorf("hata sebebi sürüm satırına yazılmadı: %q", rel.Detail)
	}
}

// TestBuildErrorTextNeverEntersTheAuditChain, kullanıcı deposundan gelen
// metnin ekle-sadece zincire GİRMEDİĞİNİ doğrular.
//
// Ayrım kasıtlı: `releases.detail` değiştirilebilir ve kullanıcının kendi
// çıktısıdır; denetim zinciri ekle-sadece'dir ve oraya bir kez yazılan
// sır GERİ ALINAMAZ — silmek zinciri koparır.
func TestBuildErrorTextNeverEntersTheAuditChain(t *testing.T) {
	const leak = "AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI"
	fe := &fakeExec{
		build: func(context.Context, *panelyv1.ImageBuildRequest, execclient.BuildSink) (string, error) {
			return "", errors.New("derleme başarısız: " + leak)
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	_ = srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st)

	for _, rec := range auditActions(t, db) {
		if strings.Contains(rec.Detail, leak) || strings.Contains(rec.ParamsJSON, leak) {
			t.Fatalf("derleme hata metni denetim zincirine sızdı (seq %d): %s | %s",
				rec.Seq, rec.Detail, rec.ParamsJSON)
		}
	}
}

// TestReleaseIsSealedEvenWhenTheClientDisconnects, mühürlemenin çağıranın
// bağlamından KOPARILDIĞINI doğrular.
//
// ── Neden bu testin var olması gerekiyor ───────────────────────────
//
// İstemcinin kopması en sık başarısızlık sebebidir ve tam o anda stream'in
// bağlamı ZATEN İPTAL OLMUŞTUR. Mühürleme o bağlamla yapılsaydı, tam da
// gerekli olduğu anda çalışmazdı: sürüm sonsuza kadar BUILDING'de asılı
// kalır ve hostta oluşmuş olabilecek imajın kontrol düzleminde karşılığı
// olmazdı. Yani hata, YALNIZCA gerçek bir kopma anında ortaya çıkardı.
func TestReleaseIsSealedEvenWhenTheClientDisconnects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	fe := &fakeExec{
		build: func(context.Context, *panelyv1.ImageBuildRequest, execclient.BuildSink) (string, error) {
			// İstemci derleme sürerken gitti.
			cancel()
			return "", context.Canceled
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(ctx)
	_ = srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st)

	rel, err := db.GetRelease(context.Background(), "blog", "r1")
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if rel.Status == store.ReleaseBuilding {
		t.Fatal("istemci koptuğunda sürüm BUILDING'de asılı kaldı — " +
			"mühürleme iptal edilmiş bağlama bağlı")
	}
	if rel.Status != store.ReleaseFailed {
		t.Errorf("sürüm durumu %d, beklenen FAILED", rel.Status)
	}
}

// TestAuditIsWrittenEvenWhenTheClientDisconnects, denetim kaydının da
// iptal edilmiş bağlamdan koparıldığını doğrular.
//
// "Kullanıcı bağlantıyı keserek kaydı engelleyebiliyor" bir denetim
// günlüğü için kabul edilemez.
func TestAuditIsWrittenEvenWhenTheClientDisconnects(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	fe := &fakeExec{
		build: func(context.Context, *panelyv1.ImageBuildRequest, execclient.BuildSink) (string, error) {
			cancel()
			return "", context.Canceled
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(ctx)
	_ = srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st)

	var found bool
	for _, rec := range auditActions(t, db) {
		if rec.Action == "app.deploy" {
			found = true
			if rec.Outcome != audit.OutcomeFailure {
				t.Errorf("kopan dağıtım %v olarak kaydedildi", rec.Outcome)
			}
		}
	}
	if !found {
		t.Fatal("istemci koptuğunda app.deploy denetime HİÇ yazılmadı")
	}
}

// TestSuccessfulDeployRecordsImageIDInAudit, kaydın YANLIŞLANABİLİR
// olduğunu doğrular: hangi imajın üretildiği sonradan hostta kontrol
// edilebilmeli.
func TestSuccessfulDeployRecordsImageIDInAudit(t *testing.T) {
	fe := &fakeExec{
		build: func(context.Context, *panelyv1.ImageBuildRequest, execclient.BuildSink) (string, error) {
			return "sha256:denetlenebilir", nil
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	if err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	for _, rec := range auditActions(t, db) {
		if rec.Action != "app.deploy" {
			continue
		}
		if rec.Outcome != audit.OutcomeSuccess {
			t.Errorf("başarılı dağıtım %v kaydedildi", rec.Outcome)
		}
		if !strings.Contains(rec.ParamsJSON, "sha256:denetlenebilir") {
			t.Errorf("imaj kimliği denetime yazılmadı: %s", rec.ParamsJSON)
		}
		if !strings.Contains(rec.ParamsJSON, apiSHA) {
			t.Errorf("commit_sha denetime yazılmadı: %s", rec.ParamsJSON)
		}
		if rec.Target != "app/blog/release/r1" {
			t.Errorf("hedef = %q", rec.Target)
		}
		return
	}
	t.Fatal("app.deploy kaydı bulunamadı")
}

// TestDeployStopsWhenTheClientCannotBeWritten, istemciye yazılamadığında
// derlemenin sürdürülmediğini doğrular.
func TestDeployStopsWhenTheClientCannotBeWritten(t *testing.T) {
	sinkErr := errors.New("istemci gitti")
	chunks := 0
	fe := &fakeExec{
		build: func(_ context.Context, _ *panelyv1.ImageBuildRequest, sink execclient.BuildSink) (string, error) {
			for range 10 {
				if err := sink([]byte("satir\n"), false); err != nil {
					return "", err
				}
				chunks++
			}
			return "sha256:abc", nil
		},
	}
	srv, db := newDeployServer(t, fe)
	mustCreateApp(t, srv, testSpec())

	st := newDeployStream(context.Background())
	st.failFrom, st.sendErr = 2, sinkErr // Accepted + 1 çıktı geçer, sonra kopar.

	if err := srv.Deploy(&panelyv1.DeployRequest{AppId: "blog", CommitSha: apiSHA}, st); err == nil {
		t.Fatal("istemci yazılamazken dağıtım başarılı sayıldı")
	}
	if chunks >= 10 {
		t.Errorf("istemci koptuktan sonra derleme sürdü (%d parça)", chunks)
	}

	rel, err := db.GetRelease(context.Background(), "blog", "r1")
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if rel.Status == store.ReleaseBuilding {
		t.Error("sürüm BUILDING'de asılı kaldı")
	}
}

// ── Dağıtımın TRAFİK yarısı ──────────────────────────────────────────

// newDeployServerWith, rollout'u çağıranın verdiği sahteyle kurar.
func newDeployServerWith(t *testing.T, fe *fakeExec, ro *fakeRollout) (*Server, *store.Store) {
	t.Helper()

	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	srv, err := NewServer(ServerOptions{
		Store: db, Executor: fe, Rollout: ro, Reconciler: &fakeReconciler{},
	})
	if err != nil {
		t.Fatalf("sunucu oluşturulamadı: %v", err)
	}
	return srv, db
}

// TestDeployHandsTheBuiltReleaseToTheRollout, derlemeden sonra trafiğin
// GERÇEKTEN taşındığını doğrular.
//
// Bu test, dilim 4a'da kasten eksik bırakılan yarının bağlandığını
// koruyor. Bağlantı olmadan Deploy "başarılı" döner, DeploySucceeded
// gönderir ve kullanıcının sitesi HİÇ AYAĞA KALKMAZ — sessiz ve en kötü
// türden bir başarısızlık.
func TestDeployHandsTheBuiltReleaseToTheRollout(t *testing.T) {
	ro := &fakeRollout{}
	srv, _ := newDeployServerWith(t, okBuild(), ro)
	mustCreateApp(t, srv, testSpec())

	stream := &deployStream{ctx: context.Background()}
	if err := srv.Deploy(&panelyv1.DeployRequest{
		AppId: "blog", CommitSha: apiSHA,
	}, stream); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	// "Çağrıldı mı" değil, HANGİ sürümle: yanlış sürümü canlıya alan bir
	// dağıtım yalnızca varlık kontrolünden geçerdi.
	if len(ro.calls) != 1 || ro.calls[0] != "blog/r1" {
		t.Fatalf("rollout çağrıları %v, [blog/r1] bekleniyordu", ro.calls)
	}
}

// TestFailedBuildNeverReachesTheRollout, derlemesi başarısız bir sürümün
// canlıya ALINMADIĞINI doğrular.
//
// Şemadaki tetikleyici de bunu engelliyor (aktif sürüm BUILT olmalı), ama
// iki savunma bir savunmadan iyi: buradaki kontrol, isteğin veritabanına
// HİÇ ULAŞMADIĞINI gösteriyor.
func TestFailedBuildNeverReachesTheRollout(t *testing.T) {
	ro := &fakeRollout{}
	srv, _ := newDeployServerWith(t,
		failBuild(), ro)
	mustCreateApp(t, srv, testSpec())

	stream := &deployStream{ctx: context.Background()}
	if err := srv.Deploy(&panelyv1.DeployRequest{
		AppId: "blog", CommitSha: apiSHA,
	}, stream); err == nil {
		t.Fatal("başarısız derleme başarı sayıldı")
	}
	if len(ro.calls) != 0 {
		t.Fatalf("derlemesi başarısız sürüm canlıya alındı: %v", ro.calls)
	}
}

// TestRolloutFailureIsNotReportedAsSuccess, trafiğin taşınamadığı bir
// dağıtımın BAŞARILI görünmediğini doğrular.
//
// DeploySucceeded'in anlamı "imaj üretildi" değil, "dağıtım tamamlandı".
// Rollout patladığı hâlde o mesajı göndermek, istemciye canlı olmayan bir
// sürümü canlı diye bildirmek olurdu.
func TestRolloutFailureIsNotReportedAsSuccess(t *testing.T) {
	ro := &fakeRollout{err: errors.New("sağlık kapısı geçilemedi")}
	srv, db := newDeployServerWith(t, okBuild(), ro)
	mustCreateApp(t, srv, testSpec())

	stream := &deployStream{ctx: context.Background()}
	err := srv.Deploy(&panelyv1.DeployRequest{
		AppId: "blog", CommitSha: apiSHA,
	}, stream)
	if err == nil {
		t.Fatal("kapıda duran dağıtım başarı sayıldı")
	}

	for _, m := range stream.sent {
		if m.GetSucceeded() != nil {
			t.Fatal("trafiği taşınmayan dağıtım için DeploySucceeded gönderildi")
		}
	}

	// İmaj GERÇEKTEN üretildi: sürüm BUILT kalmalı. Geri almak, hostta
	// duran bir imajın veritabanında karşılığını silerdi.
	rel, err := db.GetRelease(context.Background(), "blog", "r1")
	if err != nil {
		t.Fatalf("sürüm okunamadı: %v", err)
	}
	if rel.Status != store.ReleaseBuilt {
		t.Errorf("sürüm durumu %v, BUILT bekleniyordu — imaj hostta duruyor", rel.Status)
	}
}

// okBuild, aux karesinden imaj kimliği dönen bir executor taklidi.
func okBuild() *fakeExec {
	return &fakeExec{build: func(context.Context, *panelyv1.ImageBuildRequest,
		execclient.BuildSink) (string, error) {
		return "sha256:kabul", nil
	}}
}

// failBuild, derlemesi çöken bir executor taklidi.
func failBuild() *fakeExec {
	return &fakeExec{build: func(context.Context, *panelyv1.ImageBuildRequest,
		execclient.BuildSink) (string, error) {
		return "", errors.New("derleme çöktü")
	}}
}

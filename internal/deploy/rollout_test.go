package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/store"
)

// ── Sahteler ─────────────────────────────────────────────────────────

type stopCall struct {
	appID     string
	releaseID string
	grace     time.Duration
}

// fakeLifecycle, KAPININ gördüğü replikaları ve durdurma çağrılarını
// yönetir.
//
// Uzlaştırıcının replika kaynağından (fakeReplicas) KASTEN ayrı: kapı ile
// rota üretimi farklı sorular soruyor ve testin ikisini bağımsız
// sürükleyebilmesi gerekiyor.
type fakeLifecycle struct {
	replicas []execclient.Replica
	listErr  error

	created []execclient.CreateReplicaOptions
	started []string
	stopped []stopCall
	stopErr error

	// proxy, SIRA iddiası için: durdurma anında ters vekile kaç kez
	// yüklendiğini kaydediyoruz. "Boşaltma penceresi beklendi" ile
	// "durdurma Caddy yüklendikten SONRA oldu" ayrı iddialardır ve
	// yalnızca uykuları saymak ikincisini kanıtlamaz.
	proxy          *fakeProxy
	proxyLoadsThen []int
}

func (f *fakeLifecycle) EnsureNetwork(context.Context, string) (string, error) {
	return "panely-test", nil
}

func (f *fakeLifecycle) CreateReplica(_ context.Context, o execclient.CreateReplicaOptions) error {
	f.created = append(f.created, o)
	return nil
}

func (f *fakeLifecycle) StartReplica(_ context.Context, app, rel string, _ uint32) error {
	f.started = append(f.started, app+"/"+rel)
	return nil
}

func (f *fakeLifecycle) ListReplicas(context.Context, string) ([]execclient.Replica, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.replicas, nil
}

func (f *fakeLifecycle) StopRelease(
	_ context.Context, app, rel string, grace time.Duration,
) (uint32, error) {
	f.stopped = append(f.stopped, stopCall{appID: app, releaseID: rel, grace: grace})
	if f.proxy != nil {
		f.proxyLoadsThen = append(f.proxyLoadsThen, f.proxy.calls)
	}
	if f.stopErr != nil {
		return 0, f.stopErr
	}
	return 1, nil
}

// stoppedReleases, durdurulan sürüm kimliklerini döndürür.
func (f *fakeLifecycle) stoppedReleases() []string {
	out := make([]string, 0, len(f.stopped))
	for _, s := range f.stopped {
		out = append(out, s.releaseID)
	}
	return out
}

type fakeActivations struct {
	active map[string]string
	err    error
}

func (f *fakeActivations) SetActiveRelease(_ context.Context, app, rel string) error {
	if f.err != nil {
		return f.err
	}
	if f.active == nil {
		f.active = map[string]string{}
	}
	f.active[app] = rel
	return nil
}

// fakeClock, beklemeyi KAYDEDER ve sanal saati ilerletir.
//
// Gerçek uykuyu testten çıkarmak yalnızca hız için değil: boşaltma
// penceresinin GERÇEKTEN beklendiğini ancak kaydedilen süreleri
// okuyarak iddia edebiliriz.
type fakeClock struct {
	now   time.Time
	slept []time.Duration
}

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
	return nil
}

// ── Düzenek ──────────────────────────────────────────────────────────

const (
	testApp = "portfolio"
	relOld  = "r1"
	relNew  = "r2"
)

func testApplication() store.App {
	return store.App{
		ID:            testApp,
		Replicas:      1,
		ContainerPort: 8080,
		Domain:        "example.test",
		HealthPath:    "/",
	}
}

// newTestRollout, sanal saatli bir orkestratör kurar.
//
// routable: uzlaştırıcının BİZİM uygulamamızı rotalayıp rotalayamayacağı.
// false ise Reconcile onu atlar — yani trafik taşınmamış olur.
func newTestRollout(
	t *testing.T, life *fakeLifecycle, routable bool,
) (*Rollout, *fakeActivations, *fakeClock, *fakeProxy) {
	t.Helper()

	var reps []execclient.Replica
	if routable {
		reps = []execclient.Replica{running(testApp, relNew, 0, "172.20.0.3")}
	}

	proxy := &fakeProxy{}
	rec, err := New(
		fakeDeployments{{AppID: testApp, ReleaseID: relNew, Domain: "example.test", ContainerPort: 8080}},
		fakeReplicas{byApp: map[string][]execclient.Replica{testApp: reps}},
		proxy,
		testAdmin(),
	)
	if err != nil {
		t.Fatalf("uzlaştırıcı kurulamadı: %v", err)
	}

	life.proxy = proxy
	acts := &fakeActivations{}
	r, err := NewRollout(life, acts, rec, DefaultGate, DefaultDrain)
	if err != nil {
		t.Fatalf("orkestratör kurulamadı: %v", err)
	}
	clock := &fakeClock{now: time.Unix(0, 0)}
	r.clock = clock
	return r, acts, clock, proxy
}

// bothReleasesUp, hostta hem eski hem yeni sürümün ayakta olduğu durumu
// üretir — blue-green geçişinin tam ortası.
func bothReleasesUp() []execclient.Replica {
	return []execclient.Replica{
		running(testApp, relOld, 0, "172.20.0.2"),
		running(testApp, relNew, 0, "172.20.0.3"),
	}
}

// ── Testler ──────────────────────────────────────────────────────────

// Bu, GERÇEK sunucuda gözlenen kusurdur: r2 dağıtıldıktan sonra
// panely_portfolio_r1_0 28 saat boyunca ayakta kaldı.
func TestOldReleaseIsStoppedOnceTrafficHasMoved(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	r, _, _, _ := newTestRollout(t, life, true)

	if err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	got := life.stoppedReleases()
	if len(got) != 1 || got[0] != relOld {
		t.Fatalf("eski sürüm durdurulmadı: %v (beklenen [%s])", got, relOld)
	}
}

// Aktif sürümü durdurmak siteyi düşürürdü. Bu testin kırmızıya dönmesi,
// dağıtımın kendi ayağına sıkması demektir.
func TestActiveReleaseIsNeverStopped(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	r, _, _, _ := newTestRollout(t, life, true)

	if err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	for _, s := range life.stopped {
		if s.releaseID == relNew {
			t.Fatalf("AKTİF sürüm durduruldu — trafik alan konteyner öldürüldü")
		}
	}
}

// Boşaltma penceresi, Caddy yüklendikten SONRA ve durdurmadan ÖNCE
// beklenmeli: uçan istekler bitsin diye.
func TestDrainWindowIsWaitedBeforeStopping(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	r, _, clock, _ := newTestRollout(t, life, true)

	if err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	if len(clock.slept) == 0 {
		t.Fatal("hiç beklenmedi")
	}
	last := clock.slept[len(clock.slept)-1]
	if last != DefaultDrain.Window {
		t.Fatalf("son bekleme %v, boşaltma penceresi %v olmalıydı", last, DefaultDrain.Window)
	}
	if len(life.stopped) == 0 {
		t.Fatal("boşaltmadan sonra durdurma yapılmadı")
	}

	// ⚠ Asıl iddia bu: durdurma, ters vekil YÜKLENDİKTEN sonra oldu.
	// Uykuları saymak bunu kanıtlamaz — boşaltma yanlışlıkla Caddy'den
	// önce yapılsaydı süreler yine aynı görünürdü, ama o an trafiği ALAN
	// konteyner öldürülmüş olurdu.
	for i, loads := range life.proxyLoadsThen {
		if loads < 1 {
			t.Fatalf("durdurma #%d ters vekil YÜKLENMEDEN yapıldı — "+
				"trafiği alan konteyner öldürülürdü", i)
		}
	}
}

// Sağlık kapısı düşerse trafik taşınmaz — dolayısıyla eski sürüm HÂLÂ
// canlıdır ve durdurulması siteyi düşürür.
func TestNothingIsStoppedWhenTheGateFails(t *testing.T) {
	// Yeni sürümün replikası EXITED: kapıdan geçemez.
	life := &fakeLifecycle{replicas: []execclient.Replica{
		running(testApp, relOld, 0, "172.20.0.2"),
		{
			AppID: testApp, ReleaseID: relNew, Index: 0,
			State: panelyv1.ContainerState_CONTAINER_STATE_EXITED,
		},
	}}
	r, acts, _, _ := newTestRollout(t, life, true)

	err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew})
	if err == nil {
		t.Fatal("kapı düşmesine rağmen dağıtım başarılı sayıldı")
	}
	if len(life.stopped) != 0 {
		t.Fatalf("kapı düştüğü hâlde durduruldu: %v", life.stoppedReleases())
	}
	if len(acts.active) != 0 {
		t.Fatalf("kapı düştüğü hâlde trafik devredildi: %v", acts.active)
	}
}

// En ince durum: yükleme başarılı ama BİZİM uygulamamız rotalanamadı.
// Trafik taşınmadığı için eski sürüm ayakta kalmalı.
func TestOldReleaseSurvivesWhenOurOwnAppWasSkipped(t *testing.T) {
	life := &fakeLifecycle{replicas: bothReleasesUp()}
	// routable=false: uzlaştırıcı bizim uygulamamızı atlar.
	r, _, _, _ := newTestRollout(t, life, false)

	err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew})
	if err == nil {
		t.Fatal("uygulama rotalanamadığı hâlde dağıtım başarılı sayıldı")
	}
	var skipped SkippedError
	if !errors.As(err, &skipped) {
		t.Fatalf("beklenen SkippedError, gelen: %T %v", err, err)
	}
	if len(life.stopped) != 0 {
		t.Fatalf("rotalanmayan uygulamanın eski sürümü durduruldu: %v — SİTE DÜŞERDİ",
			life.stoppedReleases())
	}
}

// Durdurma başarısız olursa dağıtım BAŞARISIZ sayılmamalı: trafik zaten
// taşındı. Ama sessiz de kalmamalı.
func TestStopFailureIsReportedWithoutFailingTheDeploy(t *testing.T) {
	life := &fakeLifecycle{
		replicas: bothReleasesUp(),
		stopErr:  errors.New("docker cevap vermedi"),
	}
	r, acts, _, _ := newTestRollout(t, life, true)

	err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew})
	if err == nil {
		t.Fatal("durdurma hatası sessizce yutuldu")
	}
	var drainErr DrainError
	if !errors.As(err, &drainErr) {
		t.Fatalf("beklenen DrainError, gelen: %T %v", err, err)
	}
	if !strings.Contains(err.Error(), "docker cevap vermedi") {
		t.Fatalf("altta yatan sebep kayboldu: %v", err)
	}
	// Trafik taşınmış olmalı — bu bir dağıtım başarısızlığı DEĞİL.
	if acts.active[testApp] != relNew {
		t.Fatalf("trafik taşınmadı: %v", acts.active)
	}
}

// Aynı sürümün birden çok replikası, tek durdurma çağrısı üretmeli.
func TestEachStaleReleaseIsStoppedOnce(t *testing.T) {
	life := &fakeLifecycle{replicas: []execclient.Replica{
		running(testApp, relOld, 0, "172.20.0.2"),
		running(testApp, relOld, 1, "172.20.0.4"),
		running(testApp, "r0", 0, "172.20.0.5"),
		running(testApp, relNew, 0, "172.20.0.3"),
	}}
	r, _, _, _ := newTestRollout(t, life, true)

	if err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew}); err != nil {
		t.Fatalf("dağıtım başarısız: %v", err)
	}

	got := life.stoppedReleases()
	if len(got) != 2 {
		t.Fatalf("beklenen 2 durdurma (r0, r1), gelen: %v", got)
	}
	// Belirlenimli sıra: günlükler karşılaştırılabilir olsun.
	if got[0] != "r0" || got[1] != relOld {
		t.Fatalf("durdurma sırası belirlenimli değil: %v", got)
	}
}

// ── Kapının kendisi ──────────────────────────────────────────────────
//
// Bu davranış bugüne kadar YALNIZCA internal/api üzerinden dolaylı
// sınanıyordu; kapının en kritik özelliği hiçbir testin doğrudan hedefi
// değildi.

// Ardışık sayaç, ilk başarısız ölçümde SIFIRLANMALI. Açılışta bir süre
// RUNNING görünüp sonra ölen bir konteyner, sıfırlama olmasaydı kapıdan
// geçerdi.
func TestReadyStreakResetsOnAnyFailedProbe(t *testing.T) {
	life := &flappingLifecycle{
		// hazır, hazır, ÖLDÜ, hazır, hazır, hazır
		script: []bool{true, true, false, true, true, true},
	}
	r, _, clock, _ := newTestRollout(t, &fakeLifecycle{}, true)
	r.lifecycle = life

	if err := r.Run(context.Background(), testApplication(), store.Release{ID: relNew}); err != nil {
		t.Fatalf("kapı geçilemedi: %v", err)
	}

	// Sıfırlama olmasaydı 3. ölçümde (indeks 2) geçerdi. Sıfırlandığı
	// için 6. ölçüme kadar sürmeli.
	if life.probes < 6 {
		t.Fatalf("kapı %d ölçümde geçti — sayaç SIFIRLANMIYOR", life.probes)
	}
	_ = clock
}

// flappingLifecycle, ölçüm başına hazır/değil senaryosu oynatır.
type flappingLifecycle struct {
	fakeLifecycle
	script []bool
	probes int
}

func (f *flappingLifecycle) ListReplicas(context.Context, string) ([]execclient.Replica, error) {
	i := f.probes
	f.probes++
	if i >= len(f.script) {
		i = len(f.script) - 1
	}
	if f.script[i] {
		return []execclient.Replica{running(testApp, relNew, 0, "172.20.0.3")}, nil
	}
	return []execclient.Replica{{
		AppID: testApp, ReleaseID: relNew, Index: 0,
		State: panelyv1.ContainerState_CONTAINER_STATE_EXITED,
	}}, nil
}

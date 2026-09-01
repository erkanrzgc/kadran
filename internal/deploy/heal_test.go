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

// ── healWorld: Docker'ın TEK gerçekliği ──────────────────────────────
//
// Dosyanın geri kalanındaki sahtelerde kapının gördüğü replikalar
// (fakeLifecycle) ile uzlaştırıcının gördükleri (fakeReplicas) KASTEN
// ayrı: iki farklı soru soruyorlar ve dağıtım testlerinin ikisini
// bağımsız sürükleyebilmesi gerekiyor.
//
// İYİLEŞTİRMEDE bu ayrım yanıltıcı olur. Gerçekte ikisi de aynı Docker'a
// bakıyor ve `StartReplica` ikisini BİRDEN değiştiriyor. Ayrı tutsaydım,
// konteyneri hiç başlatmayan bir `Heal` bile uzlaştırmayı geçerdi —
// yani sahtenin kendisi hatayı gizlerdi (K-073'ün şekli).
//
// Bu yüzden healWorld hem Lifecycle hem Reconciler'ın replika kaynağı.
type healWorld struct {
	replicas []execclient.Replica

	started   []string
	createdAt []execclient.CreateReplicaOptions
	stopped   []string
	networks  int

	// startFails, başlatmanın konteyneri ayağa kaldırmamasını sağlar:
	// çağrı kaydedilir ama durum DEĞİŞMEZ. Gerçekte de olur (imaj
	// bozuk, port dolu) ve iyileştirmenin bunu başarı sayması, çökmüş
	// bir uygulamayı iyileşti diye kaydettirirdi.
	startFails bool
}

func (w *healWorld) EnsureNetwork(context.Context, string) (string, error) {
	w.networks++
	return "panely-" + testApp, nil
}

func (w *healWorld) CreateReplica(_ context.Context, o execclient.CreateReplicaOptions) error {
	w.createdAt = append(w.createdAt, o)
	// Yaratılan konteyner DURUYOR: Docker'da `create` başlatmaz. Başlatma
	// ayrı bir adım ve testin onu ayrıca doğrulaması gerekiyor.
	w.replicas = append(w.replicas, execclient.Replica{
		AppID: o.AppID, ReleaseID: o.ReleaseID, Index: o.Index,
		State: panelyv1.ContainerState_CONTAINER_STATE_EXITED,
	})
	return nil
}

func (w *healWorld) StartReplica(_ context.Context, app, rel string, idx uint32) error {
	w.started = append(w.started, app+"/"+rel)
	if w.startFails {
		return nil
	}
	for i := range w.replicas {
		r := &w.replicas[i]
		if r.AppID == app && r.ReleaseID == rel && r.Index == idx {
			r.State = panelyv1.ContainerState_CONTAINER_STATE_RUNNING
			// Adres, konteyner AYAĞA KALKINCA doğuyor. Duran bir
			// konteynerin IP'si yoktur (gerçek sunucuda ölçüldü:
			// `docker inspect` "invalid IP" döndürüyor).
			r.IPAddress = healIP
		}
	}
	return nil
}

func (w *healWorld) ListReplicas(context.Context, string) ([]execclient.Replica, error) {
	out := make([]execclient.Replica, len(w.replicas))
	copy(out, w.replicas)
	return out, nil
}

func (w *healWorld) StopRelease(
	_ context.Context, _, rel string, _ time.Duration,
) (uint32, error) {
	w.stopped = append(w.stopped, rel)
	return 1, nil
}

const healIP = "172.20.0.7"

// stoppedReplica, öldürülmüş bir konteyneri temsil eder: kayıt duruyor,
// durum EXITED, adres YOK. `docker kill` sonrası gerçek durum budur.
func stoppedReplica(rel string, idx uint32) execclient.Replica {
	return execclient.Replica{
		AppID: testApp, ReleaseID: rel, Index: idx,
		State: panelyv1.ContainerState_CONTAINER_STATE_EXITED,
	}
}

type healHarness struct {
	rollout *Rollout
	world   *healWorld
	proxy   *fakeProxy
	acts    *fakeActivations
	prober  *fakeProber
	clock   *fakeClock
	app     store.App
	rel     store.Release
}

func newHealHarness(t *testing.T, world *healWorld) *healHarness {
	t.Helper()

	proxy := &fakeProxy{}
	rec, err := New(
		fakeDeployments{{
			AppID: testApp, ReleaseID: relNew,
			Domain: "example.test", ContainerPort: 8080,
		}},
		world, // ← uzlaştırıcı da AYNI dünyayı okuyor
		proxy,
		testAdmin(),
	)
	if err != nil {
		t.Fatalf("uzlaştırıcı kurulamadı: %v", err)
	}

	acts := &fakeActivations{}
	prober := &fakeProber{}
	r, err := NewRollout(world, acts, rec, prober, DefaultGate, DefaultDrain)
	if err != nil {
		t.Fatalf("orkestratör kurulamadı: %v", err)
	}
	clock := &fakeClock{now: time.Unix(0, 0)}
	r.clock = clock

	app := testApplication()
	app.HealthPath = "/"

	return &healHarness{
		rollout: r, world: world, proxy: proxy, acts: acts,
		prober: prober, clock: clock, app: app,
		rel: store.Release{AppID: testApp, ID: relNew, CommitSHA: strings.Repeat("b", 40)},
	}
}

// TestHealRestartsStoppedContainerAndReroutes, iyileştirmenin TAM
// zincirini doğrular: duran konteyner başlatılır, kapıdan geçer ve ters
// vekile YAZILIR.
//
// Son adım kritik: yalnızca konteyneri başlatan bir gözetmen `docker ps`
// çıktısını düzeltir ama site 502 dönmeye devam eder — çünkü Caddy'nin
// upstream'i eski (ölü) adrese bakıyordur.
func TestHealRestartsStoppedContainerAndReroutes(t *testing.T) {
	w := &healWorld{replicas: []execclient.Replica{stoppedReplica(relNew, 0)}}
	h := newHealHarness(t, w)

	// Ön koşul: uygulama GERÇEKTEN çökmüş durumda.
	if ready, _ := h.rollout.Check(context.Background(), h.app, relNew); ready != 0 {
		t.Fatalf("ön koşul bozuk: hazır replika %d, 0 bekleniyordu", ready)
	}

	recreated, err := h.rollout.Heal(context.Background(), h.app, h.rel)
	if err != nil {
		t.Fatalf("iyileştirme başarısız: %v", err)
	}
	if recreated {
		t.Error("konteyner duruyordu, YENİDEN KURULMAMALIYDI — " +
			"imajdan kurmak dakikalar sürer, ölçüt 30 saniye")
	}
	if len(w.started) != 1 {
		t.Errorf("başlatma çağrısı %d, 1 bekleniyordu: %v", len(w.started), w.started)
	}

	// Konteyner ayağa kalktı mı?
	if ready, why := h.rollout.Check(context.Background(), h.app, relNew); ready != 1 {
		t.Errorf("iyileştirmeden sonra hazır replika %d, 1 bekleniyordu (%s)", ready, why)
	}

	// ── ASIL İDDİA: ters vekil güncellendi ──────────────────────────
	if h.proxy.calls == 0 {
		t.Fatal("ters vekile HİÇ yüklenmedi — konteyner ayakta ama Caddy " +
			"hâlâ eski upstream'e bakıyor, site dışarıdan erişilemez")
	}
	got := hosts(t, h.proxy.loaded)
	ups := got["example.test"]
	if len(ups) != 1 || !strings.Contains(ups[0], healIP) {
		t.Errorf("upstream %v, %q içermeliydi", ups, healIP)
	}
}

// TestHealDoesNotTouchTrafficOwnership, iyileştirmenin dağıtımın
// KUYRUĞUNU çalıştırmadığını doğrular.
//
// `SetActiveRelease` burada anlamsız (sürüm zaten aktif) ama `drainStale`
// TEHLİKELİ: çökmüş bir uygulamayı kurtarırken başka sürümleri
// durdurmanın hiçbir gerekçesi yok. Bu test, `Heal`'in bir gün
// kolaylık olsun diye `switchTraffic`e bağlanmasını engelliyor.
func TestHealDoesNotTouchTrafficOwnership(t *testing.T) {
	w := &healWorld{replicas: []execclient.Replica{
		stoppedReplica(relNew, 0),
		// Eski sürümün konteyneri de duruyor: geri alma bunu bekliyor.
		stoppedReplica(relOld, 0),
	}}
	h := newHealHarness(t, w)

	if _, err := h.rollout.Heal(context.Background(), h.app, h.rel); err != nil {
		t.Fatalf("iyileştirme başarısız: %v", err)
	}

	if len(w.stopped) != 0 {
		t.Errorf("iyileştirme sürüm durdurdu: %v — boşaltma iyileştirmenin işi DEĞİL "+
			"ve durdurulan sürüm, geri almanın saniyeler içinde döneceği sürümdü", w.stopped)
	}
	if len(h.acts.active) != 0 {
		t.Errorf("iyileştirme aktif sürümü yazdı: %v — sürüm zaten aktifti", h.acts.active)
	}
}

// TestHealRecreatesVanishedContainer, konteyner kaydı TAMAMEN yok
// olduğunda imajdan kurulduğunu doğrular.
//
// Host yeniden başladığında ya da biri `docker rm` çalıştırdığında olur.
// Başlatmak imkânsızdır — başlatılacak bir şey yoktur.
func TestHealRecreatesVanishedContainer(t *testing.T) {
	w := &healWorld{} // hiç replika yok
	h := newHealHarness(t, w)

	recreated, err := h.rollout.Heal(context.Background(), h.app, h.rel)
	if err != nil {
		t.Fatalf("iyileştirme başarısız: %v", err)
	}
	if !recreated {
		t.Error("konteyner yoktu, yeniden kurulduğu BİLDİRİLMELİYDİ — " +
			"çağıran bunu denetim kaydına yazıyor ve süre farkı 100 kat")
	}
	if len(w.createdAt) != 1 {
		t.Fatalf("kurma çağrısı %d, 1 bekleniyordu", len(w.createdAt))
	}
	if got := w.createdAt[0].CommitSHA; got != h.rel.CommitSHA {
		t.Errorf("commit %q, %q bekleniyordu — yanlış commit'ten kurmak "+
			"sessizce BAŞKA bir sürümü canlıya alırdı", got, h.rel.CommitSHA)
	}
}

// TestHealFailsWhenContainerNeverAnswers, başlatma çağrısı hata
// döndürmese bile konteyner cevap vermiyorsa iyileştirmenin BAŞARISIZ
// sayıldığını doğrular.
//
// ── Ve ters vekile DOKUNULMAMALI ────────────────────────────────────
//
// `Reconcile` yapılandırmayı sıfırdan kuruyor; rotalanabilir replikası
// olmayan uygulamayı atlıyor. Burada uzlaştırsaydık uygulamayı
// yapılandırmadan DÜŞÜRÜRDÜK — yani iyileştirme girişimi durumu
// kötüleştirirdi.
func TestHealFailsWhenContainerNeverAnswers(t *testing.T) {
	w := &healWorld{
		replicas:   []execclient.Replica{stoppedReplica(relNew, 0)},
		startFails: true,
	}
	h := newHealHarness(t, w)

	if _, err := h.rollout.Heal(context.Background(), h.app, h.rel); err == nil {
		t.Fatal("konteyner hiç ayağa kalkmadı ama iyileştirme BAŞARILI döndü")
	} else if !strings.Contains(err.Error(), "kapı") {
		t.Errorf("hata %q — kapıda durduğunu söylemeliydi", err)
	}

	if h.proxy.calls != 0 {
		t.Error("cevap vermeyen uygulama için ters vekile yüklendi — " +
			"uygulama yapılandırmadan düşer, iyileştirme durumu KÖTÜLEŞTİRİR")
	}
}

// TestHealFailsWhenProxySkipsApp, uzlaştırma hata döndürmese bile BİZİM
// uygulamamız atlandıysa iyileştirmenin başarısız sayıldığını doğrular.
//
// K-042: başarı POZİTİF bir sinyal olmalı. "Uzlaştırma patlamadı",
// uygulamanın rotalandığını kanıtlamaz — atlananlar Result'ta döner ve
// atlanmış bir uygulama dışarıdan hâlâ erişilemez.
func TestHealFailsWhenProxySkipsApp(t *testing.T) {
	w := &healWorld{replicas: []execclient.Replica{stoppedReplica(relNew, 0)}}
	h := newHealHarness(t, w)

	// Uzlaştırıcıya BAŞKA bir sürüm aktifmiş gibi söyleniyor: bizim
	// sürümümüzün replikası ona göre yok, uygulama atlanır.
	rec, err := New(
		fakeDeployments{{
			AppID: testApp, ReleaseID: "r99",
			Domain: "example.test", ContainerPort: 8080,
		}},
		w, h.proxy, testAdmin(),
	)
	if err != nil {
		t.Fatalf("uzlaştırıcı kurulamadı: %v", err)
	}
	h.rollout.rec = rec

	_, err = h.rollout.Heal(context.Background(), h.app, h.rel)
	if err == nil {
		t.Fatal("uygulama ters vekilde ATLANDI ama iyileştirme başarılı döndü")
	}
	if !strings.Contains(err.Error(), "ters vekile yazılamadı") {
		t.Errorf("hata %q — atlandığını söylemeliydi", err)
	}
}

// TestHealUsesShortGateNotDeployGate, iyileştirmenin dağıtım kapısının
// süre sınırını MİRAS ALMADIĞINI doğrular.
//
// Dağıtım kapısı 90 saniye; ölçüt #3 ise 30 saniye. Kapı alandan
// okunsaydı, tek bir yavaş konteyner ölçütü sessizce kaçırırdı ve testler
// yeşil kalırdı — çünkü sahte saat gerçek süreyi ölçmez.
func TestHealUsesShortGateNotDeployGate(t *testing.T) {
	w := &healWorld{
		replicas:   []execclient.Replica{stoppedReplica(relNew, 0)},
		startFails: true,
	}
	h := newHealHarness(t, w)

	start := h.clock.now
	if _, err := h.rollout.Heal(context.Background(), h.app, h.rel); err == nil {
		t.Fatal("kapının dolması bekleniyordu")
	}
	elapsed := h.clock.now.Sub(start)

	if elapsed > DefaultHealGate.Timeout+DefaultHealGate.Interval {
		t.Errorf("kapı %v bekledi, iyileştirme sınırı %v — dağıtım kapısını (%v) miras aldı",
			elapsed, DefaultHealGate.Timeout, DefaultGate.Timeout)
	}
	if elapsed >= DefaultGate.Timeout {
		t.Errorf("kapı dağıtım sınırına (%v) kadar bekledi", DefaultGate.Timeout)
	}
}

// TestHealPropagatesNetworkFailure, ağ kurulamadığında iyileştirmenin
// hiçbir konteynere dokunmadan durduğunu doğrular.
func TestHealPropagatesNetworkFailure(t *testing.T) {
	w := &failingNetworkWorld{healWorld: healWorld{
		replicas: []execclient.Replica{stoppedReplica(relNew, 0)},
	}}
	h := newHealHarness(t, &w.healWorld)
	h.rollout.lifecycle = w

	if _, err := h.rollout.Heal(context.Background(), h.app, h.rel); err == nil {
		t.Fatal("ağ kurulamadı ama iyileştirme başarılı döndü")
	}
	if len(w.started) != 0 {
		t.Error("ağ yokken konteyner başlatıldı")
	}
}

type failingNetworkWorld struct{ healWorld }

func (w *failingNetworkWorld) EnsureNetwork(context.Context, string) (string, error) {
	return "", errors.New("ağ kurulamadı")
}

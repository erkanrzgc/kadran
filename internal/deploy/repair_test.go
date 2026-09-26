package deploy

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/proxydrv"
	"github.com/erkanrzgc/panely/internal/store"
)

// K-055: ters vekil `--resume` kullanmıyor ve "açılışta VE ters vekil
// yeniden başladığında" uzlaştırılmak zorunda. İkinci yarı hiç
// yapılmamıştı: taze sunucu testinde (K-112) yalnızca Caddy yeniden
// başlatıldı ve site 40 saniyenin 40'ında da kapalı kaldı — panelyd
// yeniden başlayana kadar. Repair o yarı.

func ikiUygulama() (fakeDeployments, fakeReplicas) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080, Replicas: 1},
		{AppID: "shop", ReleaseID: "r7", Domain: "shop.example.com", ContainerPort: 3000, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {running("blog", "r2", 0, "172.18.0.5")},
		"shop": {running("shop", "r7", 0, "172.19.0.4")},
	}}
	return deps, reps
}

// TestRepairReloadsWhenProxyLostItsRoutes: Caddy rotasız açıldı (yeniden
// başlatma sonrası tam olarak ölçülen hâl) → bir kez yüklenir.
func TestRepairReloadsWhenProxyLostItsRoutes(t *testing.T) {
	deps, reps := ikiUygulama()
	proxy := &fakeProxy{live: &proxydrv.Config{}}

	eksik, err := mustReconciler(t, deps, reps, proxy).Repair(context.Background())
	if err != nil {
		t.Fatalf("onarılamadı: %v", err)
	}
	if !slices.Equal(eksik, []string{"blog.example.com", "shop.example.com"}) {
		t.Errorf("eksik = %v", eksik)
	}
	if proxy.calls != 1 {
		t.Fatalf("Load %d kez çağrıldı, 1 bekleniyordu", proxy.calls)
	}
	if h := hosts(t, proxy.loaded); len(h) != 2 {
		t.Errorf("yüklenen yapılandırma iki uygulamayı taşımıyor: %v", h)
	}
}

// TestRepairLeavesACompleteProxyAlone: her şey yerindeyse HİÇ yüklenmez.
// İzleyici on saniyede bir koşuyor; her turda yüklemek, Caddy'yi
// durmadan yeniden yapılandırmak olurdu.
func TestRepairLeavesACompleteProxyAlone(t *testing.T) {
	deps, reps := ikiUygulama()
	proxy := &fakeProxy{}
	rc := mustReconciler(t, deps, reps, proxy)
	if _, err := rc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	proxy.live, proxy.calls = proxy.loaded, 0

	eksik, err := rc.Repair(context.Background())
	if err != nil || len(eksik) != 0 || proxy.calls != 0 {
		t.Errorf("sağlam vekil yeniden yüklendi: eksik=%v err=%v Load=%d", eksik, err, proxy.calls)
	}
}

// TestRepairKeepsASkippedAppsRoute: uzlaştırma sağlıksız "shop"u
// ATLIYOR; canlıdaki eski rotası iyileştirme bitene kadar durmalı.
// Repair iki yönlü karşılaştırsaydı o rotayı silerdi.
func TestRepairKeepsASkippedAppsRoute(t *testing.T) {
	deps, reps := ikiUygulama()
	proxy := &fakeProxy{}
	rc := mustReconciler(t, deps, reps, proxy)
	if _, err := rc.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	ikisiCanli := proxy.loaded

	reps.byApp["shop"] = nil // shop'un ayakta replikası kalmadı
	proxy.live, proxy.calls = ikisiCanli, 0

	if _, err := rc.Repair(context.Background()); err != nil {
		t.Fatal(err)
	}
	if proxy.calls != 0 {
		t.Error("atlanan uygulamanın canlı rotası silindi — iyileştirme davranışı değişti")
	}
}

// TestRepairReportsUnreadableProxy: canlı okunamıyorsa hata döner ve
// körlemesine yükleme YAPILMAZ.
func TestRepairReportsUnreadableProxy(t *testing.T) {
	deps, reps := ikiUygulama()
	proxy := &fakeProxy{currentErr: errors.New("admin soketine ulaşılamadı")}

	if _, err := mustReconciler(t, deps, reps, proxy).Repair(context.Background()); err == nil {
		t.Error("okunamayan vekil hata vermedi")
	}
	if proxy.calls != 0 {
		t.Error("canlı okunamadan yükleme yapıldı")
	}
}

// ── Eşzamanlılık ─────────────────────────────────────────────────────

// surumluDagitimlar, testin ortasında değiştirilebilen dağıtım durumu.
type surumluDagitimlar struct {
	mu   sync.Mutex
	deps []store.Deployment
}

func (s *surumluDagitimlar) ActiveDeployments(context.Context) ([]store.Deployment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.deps), nil
}

func (s *surumluDagitimlar) ayarla(d []store.Deployment) {
	s.mu.Lock()
	s.deps = d
	s.mu.Unlock()
}

// kapiliVekil, ESKİ yapılandırmanın yüklenmesini test serbest bırakana
// kadar bekletir ve yüklemelerin SIRASINI kaydeder.
type kapiliVekil struct {
	mu        sync.Mutex
	sira      []string // yüklenen yapılandırmadaki blog upstream'i
	eskiGeldi chan struct{}
	birak     chan struct{}
}

func (k *kapiliVekil) Current(context.Context) (*proxydrv.Config, error) {
	return &proxydrv.Config{}, nil // Caddy rotasız: Repair yükleyecek
}

func (k *kapiliVekil) Load(_ context.Context, cfg *proxydrv.Config) error {
	var dial string
	for _, srv := range cfg.Apps.HTTP.Servers {
		for _, r := range srv.Routes {
			for _, h := range r.Handle {
				for _, u := range h.Upstreams {
					dial = u.Dial
				}
			}
		}
	}
	if dial == "172.18.0.5:8080" { // eski sürüm
		close(k.eskiGeldi)
		<-k.birak
	}
	k.mu.Lock()
	k.sira = append(k.sira, dial)
	k.mu.Unlock()
	return nil
}

// TestRepairAndReconcileAreSerialized, izleyici ile bir dağıtımın
// birbirinin yüklemesini EZEMEYECEĞİNİ doğrular.
//
// Kilit olmadan: izleyici yapılandırmayı SetActiveRelease'ten ÖNCEKİ
// durumdan kurar, dağıtım yeni durumu yükler, sonra izleyicinin eski
// yüklemesi gelir ve trafiği boşaltılmakta olan sürüme geri çevirir.
// Kilitle: dağıtımın uzlaştırması izleyicinin yüklemesi bitene kadar
// bekler ve SON yükleme yeni durumu taşır.
func TestRepairAndReconcileAreSerialized(t *testing.T) {
	deps := &surumluDagitimlar{deps: []store.Deployment{
		{AppID: "blog", ReleaseID: "r1", Domain: "blog.example.com", ContainerPort: 8080, Replicas: 1},
	}}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{"blog": {
		running("blog", "r1", 0, "172.18.0.5"),
		running("blog", "r2", 0, "172.18.0.9"),
	}}}
	vekil := &kapiliVekil{eskiGeldi: make(chan struct{}), birak: make(chan struct{})}
	rc, err := New(deps, reps, vekil, testAdmin())
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _, _ = rc.Repair(context.Background()) }()
	<-vekil.eskiGeldi // izleyici ESKİ durumu kurdu ve yüklüyor

	// Dağıtım: aktif sürüm r2'ye geçti, uzlaştırma başlıyor.
	deps.ayarla([]store.Deployment{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080, Replicas: 1},
	})
	go func() { defer wg.Done(); _, _ = rc.Reconcile(context.Background()) }()

	time.Sleep(100 * time.Millisecond) // kilit yoksa dağıtımın yüklemesi şimdi biter
	close(vekil.birak)
	wg.Wait()

	vekil.mu.Lock()
	defer vekil.mu.Unlock()
	if n := len(vekil.sira); n != 2 || vekil.sira[n-1] != "172.18.0.9:8080" {
		t.Errorf("yükleme sırası %v — SON yükleme yeni sürümü (172.18.0.9) taşımalı", vekil.sira)
	}
}

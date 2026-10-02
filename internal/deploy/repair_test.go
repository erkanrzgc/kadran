package deploy

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/execclient"
	"github.com/erkanrzgc/kadran/internal/proxydrv"
	"github.com/erkanrzgc/kadran/internal/store"
)

// K-055: ters vekil `--resume` kullanmıyor ve "açılışta VE ters vekil
// yeniden başladığında" uzlaştırılmak zorunda. İkinci yarı hiç
// yapılmamıştı: taze sunucu testinde (K-112) yalnızca Caddy yeniden
// başlatıldı ve site 40 saniyenin 40'ında da kapalı kaldı — kadrand
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

	sonuc, err := mustReconciler(t, deps, reps, proxy).Repair(context.Background())
	eksik := sonuc.Missing
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
	// Load iki yönlü geri okuyarak doğruluyor (K-054): yüklendiyse birebir.
	if !sonuc.Exact {
		t.Error("başarılı yüklemeden sonra canlı 'birebir' sayılmadı")
	}
}

// ── Birebir eşleşme: izleyici alarmı ne zaman kapatabilir ────────────
//
// Taze sunucuda reboot'tan sonra ölçüldü (K-112): konteyner kapalı
// açıldı, açılış uzlaştırması uygulamayı atlayıp kritik alarm açtı,
// gözetmen 11 sn sonra iyileştirip rotayı yükledi — alarm dakikalarca
// AÇIK kaldı, çünkü onu kapatabilecek tek taraf izleyiciydi ve izleyici
// "ben onarmadım" diye dokunmuyordu.
//
// Ama açılış alarmı canlıda kadrand'nin GÖNDERMEDİĞİ bir rota yüzünden
// de açılıyor (Load'un geri okuması, K-054). O hâlde hiçbir şey eksik
// değil ve hiçbir uygulama atlanmıyor; "eksik yok, atlanan yok" diye
// kapatmak güvenlikle ilgili bir alarmı sessizce kapatırdı. Kapatma
// koşulu bu yüzden İKİ YÖNLÜ birebir eşleşme.

func rota(app, domain, dial string) proxydrv.AppRoute {
	return proxydrv.AppRoute{AppID: app, Domain: domain,
		Upstreams: []proxydrv.Upstream{{Dial: dial}}}
}

func canli(t *testing.T, rotalar ...proxydrv.AppRoute) *proxydrv.Config {
	t.Helper()
	cfg, err := proxydrv.BuildConfig(proxydrv.BuildOptions{Admin: testAdmin(), Routes: rotalar})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// repairOn, canlısı verilen vekil üzerinde bir onarım turu koşar ve
// durumun gerçekten "eksik yok, atlanan yok" olduğunu da doğrular —
// aksi hâlde test, saf kuralın kapatacağı durumu kurmamış olurdu.
func repairOn(t *testing.T, live *proxydrv.Config) (RepairResult, *fakeProxy) {
	t.Helper()
	deps, reps := ikiUygulama()
	proxy := &fakeProxy{live: live}
	sonuc, err := mustReconciler(t, deps, reps, proxy).Repair(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sonuc.Missing) != 0 || len(sonuc.Skipped) != 0 {
		t.Fatalf("kurulum yanlış: eksik=%v atlanan=%v", sonuc.Missing, sonuc.Skipped)
	}
	return sonuc, proxy
}

func TestRepairReportsAnExactMatch(t *testing.T) {
	sonuc, _ := repairOn(t, canli(t,
		rota("blog", "blog.example.com", "172.18.0.5:8080"),
		rota("shop", "shop.example.com", "172.19.0.4:3000")))
	if !sonuc.Exact {
		t.Error("canlı beklenenle birebir aynıyken 'birebir' sayılmadı")
	}
}

// TestRepairReportsAForeignRoute: admin soketine başkası yazmış.
func TestRepairReportsAForeignRoute(t *testing.T) {
	sonuc, proxy := repairOn(t, canli(t,
		rota("blog", "blog.example.com", "172.18.0.5:8080"),
		rota("shop", "shop.example.com", "172.19.0.4:3000"),
		rota("yabanci", "evil.example.com", "10.0.0.9:80")))
	if sonuc.Exact {
		t.Error("canlıda gönderilmeyen bir rota varken 'birebir' sayıldı")
	}
	if proxy.calls != 0 {
		t.Error("tetik tek yönlü olmalı: fazla rota yüklemeye yol açtı")
	}
}

func TestRepairReportsADifferentUpstream(t *testing.T) {
	sonuc, _ := repairOn(t, canli(t,
		rota("blog", "blog.example.com", "172.18.0.99:8080"),
		rota("shop", "shop.example.com", "172.19.0.4:3000")))
	if sonuc.Exact {
		t.Error("upstream farklıyken 'birebir' sayıldı")
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

	sonuc, err := rc.Repair(context.Background())
	eksik := sonuc.Missing
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

// canliShop, yalnızca shop'un rotasını taşıyan canlı yapılandırma.
func canliShop(t *testing.T, dial string) *proxydrv.Config {
	t.Helper()
	cfg, err := proxydrv.BuildConfig(proxydrv.BuildOptions{
		Admin: testAdmin(),
		Routes: []proxydrv.AppRoute{{AppID: "shop", Domain: "shop.example.com",
			Upstreams: []proxydrv.Upstream{{Dial: dial}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

// TestRepairCarriesASkippedAppsLiveRoute: ONARIM yüklemesi de atlanan
// (sağlıksız) uygulamanın canlı rotasını silmemeli.
//
// Güvenlik incelemesi buldu: ilk hâli, eksik bir alan adı yüzünden
// yükleme yaparken yalnızca beklenen rotaları gönderiyordu; o an atlanan
// "shop"un hâlâ canlı olan rotası bu yüklemeyle SİLİNİYORDU. "İyileştirme
// davranışı değişmez" iddiası yalnızca hiçbir şey eksik değilken
// doğruydu.
func TestRepairCarriesASkippedAppsLiveRoute(t *testing.T) {
	deps, reps := ikiUygulama()
	reps.byApp["shop"] = nil // shop sağlıksız: atlanacak
	proxy := &fakeProxy{live: canliShop(t, "172.19.0.4:3000")}

	sonuc, err := mustReconciler(t, deps, reps, proxy).Repair(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(sonuc.Missing, []string{"blog.example.com"}) {
		t.Errorf("eksik = %v", sonuc.Missing)
	}
	if _, ok := sonuc.Skipped["shop"]; !ok {
		t.Errorf("atlanan uygulama bildirilmedi: %v", sonuc.Skipped)
	}
	h := hosts(t, proxy.loaded)
	if !slices.Equal(h["shop.example.com"], []string{"172.19.0.4:3000"}) {
		t.Errorf("atlanan uygulamanın canlı rotası yüklemede korunmadı: %v", h)
	}
	if len(h["blog.example.com"]) == 0 {
		t.Errorf("eksik rota yüklenmedi: %v", h)
	}
}

// TestRepairDropsAForgedLiveUpstream: korunan rota canlıdan geliyor ve
// canlıya başkası yazmış olabilir (K-054). Geçersiz bir adres TAŞINMAZ.
func TestRepairDropsAForgedLiveUpstream(t *testing.T) {
	deps, reps := ikiUygulama()
	reps.byApp["shop"] = nil
	proxy := &fakeProxy{live: canliShop(t, "172.19.0.4:3000")}
	// Doğrudan canlıya sahte bir adres koy (BuildConfig bunu reddederdi).
	for _, srv := range proxy.live.Apps.HTTP.Servers {
		for i := range srv.Routes {
			for j := range srv.Routes[i].Handle {
				for k := range srv.Routes[i].Handle[j].Upstreams {
					srv.Routes[i].Handle[j].Upstreams[k].Dial = "unix//run/docker.sock"
				}
			}
		}
	}

	if _, err := mustReconciler(t, deps, reps, proxy).Repair(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h := hosts(t, proxy.loaded); len(h["shop.example.com"]) != 0 {
		t.Errorf("sahte upstream taşındı: %v", h)
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

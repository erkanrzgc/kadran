package deploy

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/erkanrzgc/panely/internal/execclient"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/proxydrv"
	"github.com/erkanrzgc/panely/internal/store"
)

// ── Sahteler ─────────────────────────────────────────────────────────

type fakeDeployments []store.Deployment

func (f fakeDeployments) ActiveDeployments(context.Context) ([]store.Deployment, error) {
	return []store.Deployment(f), nil
}

type fakeReplicas struct {
	byApp map[string][]execclient.Replica
	err   error
}

func (f fakeReplicas) ListReplicas(_ context.Context, appID string) ([]execclient.Replica, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byApp[appID], nil
}

// fakeProxy, YÜKLENEN yapılandırmayı saklar. Sınanan şey "Load çağrıldı
// mı" değil, İÇİNDE NE OLDUĞU.
type fakeProxy struct {
	loaded *proxydrv.Config
	calls  int
}

func (f *fakeProxy) Load(_ context.Context, cfg *proxydrv.Config) error {
	f.calls++
	f.loaded = cfg
	return nil
}

func running(app, rel string, idx uint32, ip string) execclient.Replica {
	return execclient.Replica{
		AppID: app, ReleaseID: rel, Index: idx,
		State:     panelyv1.ContainerState_CONTAINER_STATE_RUNNING,
		IPAddress: ip,
	}
}

func testAdmin() proxydrv.Admin {
	return proxydrv.Admin{Listen: "fd/3", Origins: []string{"localhost"}}
}

// hosts, yüklenen yapılandırmadan "alan adı → upstream" çıkarır.
func hosts(t *testing.T, cfg *proxydrv.Config) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	if cfg == nil || cfg.Apps == nil || cfg.Apps.HTTP == nil {
		return out
	}
	for _, srv := range cfg.Apps.HTTP.Servers {
		for _, r := range srv.Routes {
			var dials []string
			for _, h := range r.Handle {
				for _, u := range h.Upstreams {
					dials = append(dials, u.Dial)
				}
			}
			for _, m := range r.Match {
				for _, host := range m.Host {
					out[host] = append(out[host], dials...)
				}
			}
		}
	}
	return out
}

func mustReconciler(t *testing.T, d Deployments, r Replicas, p Proxy) *Reconciler {
	t.Helper()
	rc, err := New(d, r, p, testAdmin())
	if err != nil {
		t.Fatalf("uzlaştırıcı kurulamadı: %v", err)
	}
	return rc
}

// ── Testler ──────────────────────────────────────────────────────────

// TestReconcileCarriesEveryAppNotJustTheDeployedOne, yapılandırmanın TÜM
// uygulamalardan üretildiğini doğrular.
//
// Caddy'nin POST /load ucu kök nesnenin tamamını değiştiriyor. Tek bir
// uygulamadan üretilmiş bir yapılandırma diğerlerinin rotalarını siler:
// `blog` dağıtılırken `shop` internetten düşerdi. Bu, K-054'ün üretim
// tarafındaki karşılığı — geri okuma da aynı hatayı yakalıyor, ama en
// iyisi hatanın hiç oluşmaması.
func TestReconcileCarriesEveryAppNotJustTheDeployedOne(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080},
		{AppID: "shop", ReleaseID: "r7", Domain: "shop.example.com", ContainerPort: 3000},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {running("blog", "r2", 0, "172.18.0.5")},
		"shop": {running("shop", "r7", 0, "172.19.0.4")},
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("uygulama atlandı: %s", res.Error())
	}

	got := hosts(t, proxy.loaded)
	if len(got) != 2 {
		t.Fatalf("%d rota üretildi, 2 bekleniyordu: %v", len(got), got)
	}
	if d := got["shop.example.com"]; len(d) != 1 || d[0] != "172.19.0.4:3000" {
		t.Errorf("shop rotası yanlış: %v", d)
	}
	if d := got["blog.example.com"]; len(d) != 1 || d[0] != "172.18.0.5:8080" {
		t.Errorf("blog rotası yanlış: %v", d)
	}
}

// TestOnlyTheActiveReleaseReceivesTraffic, blue-green geçişinin can alıcı
// noktasını doğrular.
//
// Geçiş sırasında hostta İKİ sürümün konteynerleri aynı anda duruyor.
// Sürüm filtresi olmasaydı trafik eski ve yeni arasında rastgele
// bölünürdü — üstelik geçiş "başarılı" görünerek. Sağlık kapısından
// geçmemiş bir sürüme tek bir isteğin bile gitmemesi gerekiyor.
func TestOnlyTheActiveReleaseReceivesTraffic(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {
			running("blog", "r1", 0, "172.18.0.2"), // ESKİ sürüm, hâlâ ayakta
			running("blog", "r2", 0, "172.18.0.5"), // aktif
			running("blog", "r3", 0, "172.18.0.9"), // YENİ, henüz onaylanmadı
		},
	}}
	proxy := &fakeProxy{}

	if _, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background()); err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}

	dials := hosts(t, proxy.loaded)["blog.example.com"]
	if len(dials) != 1 || dials[0] != "172.18.0.5:8080" {
		t.Fatalf("yalnızca aktif sürüm trafiğe alınmalıydı, alınanlar: %v", dials)
	}
}

// TestSickAppDoesNotTakeDownHealthyOnes, ayakta replikası kalmamış bir
// uygulamanın diğerlerini yayından kaldırmadığını doğrular.
//
// proxydrv upstream'siz rotayı haklı olarak reddediyor (sessiz 502
// üretirdi). Ama hasta uygulama yüzünden yüklemeyi tamamen durdurmak, TEK
// bir bozuk uygulamanın sunucudaki her siteyi düşürmesi demekti.
func TestSickAppDoesNotTakeDownHealthyOnes(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080},
		{AppID: "olu", ReleaseID: "r1", Domain: "olu.example.com", ContainerPort: 8080},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {running("blog", "r2", 0, "172.18.0.5")},
		"olu":  {}, // hiç konteyner yok
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("hasta uygulama yüzünden yükleme durdu: %v", err)
	}

	if _, ok := hosts(t, proxy.loaded)["blog.example.com"]; !ok {
		t.Error("sağlam uygulama yayından kalktı")
	}
	if _, ok := hosts(t, proxy.loaded)["olu.example.com"]; ok {
		t.Error("upstream'siz uygulama yapılandırmaya girdi — sessiz 502")
	}

	// Atlama SESSİZ olmamalı: sebebiyle birlikte raporlanmalı.
	if res.Skipped["olu"] == "" {
		t.Fatal("atlanan uygulama raporlanmadı")
	}
	if !strings.Contains(res.Error(), "olu") {
		t.Errorf("rapor uygulamayı adlandırmıyor: %q", res.Error())
	}
}

// TestReplicaWithoutAnAddressIsNotRouted, adresi henüz atanmamış bir
// konteynerin trafiğe alınmadığını doğrular.
//
// `ip_address` yeni başlamış bir konteynerde ağ kurulana kadar BOŞ
// olabilir. Bu geçici durum bir hata değil; ama boş adresle rota üretmeye
// çalışmak yapılandırmayı bozardı.
func TestReplicaWithoutAnAddressIsNotRouted(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"blog": {
			running("blog", "r2", 0, ""), // ağ henüz kurulmadı
			running("blog", "r2", 1, "172.18.0.6"),
		},
	}}
	proxy := &fakeProxy{}

	if _, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background()); err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	dials := hosts(t, proxy.loaded)["blog.example.com"]
	if len(dials) != 1 || dials[0] != "172.18.0.6:8080" {
		t.Fatalf("adressiz replika trafiğe alındı: %v", dials)
	}
}

// TestStoppedReplicaIsNotRouted, durmuş konteynerin trafiğe
// alınmadığını doğrular. RESTARTING de "çalışıyor" DEĞİLDİR.
func TestStoppedReplicaIsNotRouted(t *testing.T) {
	for _, tc := range []struct {
		ad    string
		state panelyv1.ContainerState
	}{
		{"durmuş", panelyv1.ContainerState_CONTAINER_STATE_EXITED},
		{"yeniden başlıyor", panelyv1.ContainerState_CONTAINER_STATE_RESTARTING},
		{"ölü", panelyv1.ContainerState_CONTAINER_STATE_DEAD},
	} {
		t.Run(tc.ad, func(t *testing.T) {
			rep := running("blog", "r2", 0, "172.18.0.5")
			rep.State = tc.state

			deps := fakeDeployments{
				{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080},
			}
			proxy := &fakeProxy{}
			res, err := mustReconciler(t, deps,
				fakeReplicas{byApp: map[string][]execclient.Replica{"blog": {rep}}},
				proxy).Reconcile(context.Background())
			if err != nil {
				t.Fatalf("uzlaştırılamadı: %v", err)
			}
			if _, ok := hosts(t, proxy.loaded)["blog.example.com"]; ok {
				t.Errorf("%s konteynere trafik yönlendirildi", tc.ad)
			}
			if res.Skipped["blog"] == "" {
				t.Error("atlama raporlanmadı")
			}
		})
	}
}

// TestAppWithoutDomainIsNotSkippedItIsOutOfScope, alan adı olmayan bir
// uygulamanın ARIZA olarak raporlanmadığını doğrular.
//
// Alan adı vermemek geçerli bir tercih: uygulama yalnızca iç ağdan
// erişilir. Bunu "atlandı" diye raporlamak, gerçek arızaların gürültüde
// kaybolmasına yol açardı.
func TestAppWithoutDomainIsNotSkippedItIsOutOfScope(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "worker", ReleaseID: "r1", ContainerPort: 8080}, // alan adı yok
	}
	proxy := &fakeProxy{}
	res, err := mustReconciler(t, deps,
		fakeReplicas{byApp: map[string][]execclient.Replica{}}, proxy).
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("alan adsız uygulama arıza sayıldı: %s", res.Error())
	}
	// Yükleme YİNE DE yapılmalı: admin bloğu her yüklemede gitmeli.
	if proxy.calls != 1 {
		t.Errorf("Load %d kez çağrıldı, 1 bekleniyordu", proxy.calls)
	}
}

// TestReconcilerRefusesToBuildWithoutAdmin, admin bloğu olmadan
// uzlaştırıcının hiç kurulamadığını doğrular.
//
// Admin'siz bir yapılandırma yüklenirse Caddy varsayılan TCP :2019'a
// döner ve panelyd unix soketinden bir daha ULAŞAMAZ — sistem kendini
// kalıcı olarak kilitler. Hatanın yükleme anında değil KURULUM anında
// çıkması için burada da kontrol ediliyor.
func TestReconcilerRefusesToBuildWithoutAdmin(t *testing.T) {
	_, err := New(fakeDeployments{}, fakeReplicas{}, &fakeProxy{}, proxydrv.Admin{})
	if err == nil {
		t.Fatal("admin bloğu olmadan uzlaştırıcı kuruldu")
	}
	if !strings.Contains(err.Error(), "kilitler") {
		t.Errorf("hata sonucu açıklamıyor: %v", err)
	}
}

// TestListingFailureSkipsOnlyThatApp, executor'a ulaşılamayan bir
// uygulamanın diğerlerini düşürmediğini doğrular.
func TestListingFailureSkipsOnlyThatApp(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "blog", ReleaseID: "r2", Domain: "blog.example.com", ContainerPort: 8080},
	}
	proxy := &fakeProxy{}
	res, err := mustReconciler(t, deps,
		fakeReplicas{err: errors.New("executor kapalı")}, proxy).
		Reconcile(context.Background())
	if err != nil {
		t.Fatalf("liste hatası tüm yüklemeyi durdurdu: %v", err)
	}
	if !strings.Contains(res.Skipped["blog"], "executor kapalı") {
		t.Errorf("sebep taşınmıyor: %q", res.Skipped["blog"])
	}
}

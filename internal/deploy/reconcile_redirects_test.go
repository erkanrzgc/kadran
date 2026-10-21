package deploy

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/execclient"
	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

func TestReconcileAddsRedirectRoutes(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "site", ReleaseID: "r1", Domain: "example.com", WWW: true, ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"site": {running("site", "r1", 0, "172.18.0.5")},
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if len(res.Skipped) != 0 || len(res.SkippedRedirects) != 0 {
		t.Fatalf("beklenmeyen atlama: %v %v", res.Skipped, res.SkippedRedirects)
	}
	if got := proxydrv.LiveRedirects(proxy.loaded, "example.com"); !reflect.DeepEqual(got, []string{"www.example.com"}) {
		t.Errorf("yönlendirmeler = %v, www.example.com bekleniyordu", got)
	}
	if d := hosts(t, proxy.loaded)["example.com"]; len(d) != 1 || d[0] != "172.18.0.5:8080" {
		t.Errorf("domain rotası bozuldu: %v", d)
	}
}

func TestReconcileSkipsOnlyTheCollidingRedirect(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "a", ReleaseID: "r1", Domain: "a.example.com", ContainerPort: 8080, Replicas: 1},
		{AppID: "b", ReleaseID: "r1", Domain: "b.example.com", Redirects: []string{"a.example.com"}, ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"a": {running("a", "r1", 0, "172.18.0.5")},
		"b": {running("b", "r1", 0, "172.18.0.6")},
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	got := hosts(t, proxy.loaded)
	if len(got["a.example.com"]) != 1 || len(got["b.example.com"]) != 1 {
		t.Errorf("iki domain rotası da durmalı: %v", got)
	}
	if r := proxydrv.LiveRedirects(proxy.loaded, "b.example.com"); len(r) != 0 {
		t.Errorf("çakışan yönlendirme yüklendi: %v", r)
	}
	if res.SkippedRedirects["a.example.com"] == "" {
		t.Errorf("SkippedRedirects dolu olmalı: %v", res.SkippedRedirects)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("Skipped yönlendirme için yazılmamalı: %v", res.Skipped)
	}
}

func TestReconcileSkipsPartnerOfAnotherApp(t *testing.T) {
	deps := fakeDeployments{
		{AppID: "a", ReleaseID: "r1", Domain: "example.com", WWW: true, ContainerPort: 8080, Replicas: 1},
		{AppID: "b", ReleaseID: "r1", Domain: "example.org", Redirects: []string{"www.example.com"}, ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"a": {running("a", "r1", 0, "172.18.0.5")},
		"b": {running("b", "r1", 0, "172.18.0.6")},
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if got := proxydrv.LiveRedirects(proxy.loaded, "example.com"); !reflect.DeepEqual(got, []string{"www.example.com"}) {
		t.Errorf("ilk uygulama kazanmalı: %v", got)
	}
	if r := proxydrv.LiveRedirects(proxy.loaded, "example.org"); len(r) != 0 {
		t.Errorf("ikinci uygulamanın yönlendirmesi yüklendi: %v", r)
	}
	if why := res.SkippedRedirects["www.example.com"]; !strings.Contains(why, `already redirects to "example.com"`) {
		t.Errorf("sebep yanlış: %q", why)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("Skipped boş olmalı: %v", res.Skipped)
	}
}

func TestReconcileCarriesLiveRedirectsOfSkippedApp(t *testing.T) {
	up, err := proxydrv.NewUpstream("172.18.0.5", 8080)
	if err != nil {
		t.Fatalf("upstream: %v", err)
	}
	live, err := proxydrv.BuildConfig(proxydrv.BuildOptions{
		Admin: testAdmin(),
		Routes: []proxydrv.AppRoute{{
			AppID: "site", Domain: "example.com",
			Upstreams: []proxydrv.Upstream{up},
			Redirects: []string{"www.example.com"},
		}},
	})
	if err != nil {
		t.Fatalf("canlı yapılandırma kurulamadı: %v", err)
	}
	// Yeni sürüm (r2) için hostta replika yok: uygulama atlanır.
	deps := fakeDeployments{
		{AppID: "site", ReleaseID: "r2", Domain: "example.com", WWW: true, ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"site": {running("site", "r1", 0, "172.18.0.5")},
	}}
	proxy := &fakeProxy{live: live}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if res.Skipped["site"] == "" {
		t.Fatalf("uygulama atlanmış olmalı: %v", res.Skipped)
	}
	if d := hosts(t, proxy.loaded)["example.com"]; len(d) != 1 || d[0] != "172.18.0.5:8080" {
		t.Errorf("canlı domain rotası korunmalı: %v", d)
	}
	if got := proxydrv.LiveRedirects(proxy.loaded, "example.com"); !reflect.DeepEqual(got, []string{"www.example.com"}) {
		t.Errorf("canlı yönlendirme korunmalı: %v", got)
	}
}

// skippedSite, yeni sürümü (r2) rotalanamayan "site" uygulamasının canlıda
// r1 rotası ve verilen yönlendirmeleriyle durduğu bir dünya kurar.
func skippedSite(t *testing.T, liveRedirects []string, desired fakeDeployments) (*fakeProxy, *Reconciler) {
	t.Helper()
	live := canli(t, proxydrv.AppRoute{
		AppID: "site", Domain: "example.com",
		Upstreams: []proxydrv.Upstream{{Dial: "172.18.0.5:8080"}},
		Redirects: liveRedirects,
	})
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"site": {running("site", "r1", 0, "172.18.0.5")},
	}}
	proxy := &fakeProxy{live: live}
	return proxy, mustReconciler(t, desired, reps, proxy)
}

// Atlanan uygulamada artık istenmeyen ad taşınmaz: -no-www "kaldırıldı"
// derken 308 canlıda kalmamalı. Reconcile ve Repair aynı taşıma yolunu
// kullanıyor; ikisi de sınanıyor.
func TestReconcileDropsRemovedRedirectOfSkippedApp(t *testing.T) {
	desired := fakeDeployments{
		{AppID: "site", ReleaseID: "r2", Domain: "example.com", WWW: false, ContainerPort: 8080, Replicas: 1},
	}
	for _, entry := range []string{"Reconcile", "Repair"} {
		t.Run(entry, func(t *testing.T) {
			proxy, rc := skippedSite(t, []string{"www.example.com"}, desired)
			var skipped, skippedRedirects map[string]string
			if entry == "Reconcile" {
				res, err := rc.Reconcile(context.Background())
				if err != nil {
					t.Fatalf("uzlaştırılamadı: %v", err)
				}
				skipped, skippedRedirects = res.Skipped, res.SkippedRedirects
			} else {
				res, err := rc.Repair(context.Background())
				if err != nil {
					t.Fatalf("onarılamadı: %v", err)
				}
				skipped = res.Skipped
			}
			if skipped["site"] == "" {
				t.Fatalf("uygulama atlanmış olmalı: %v", skipped)
			}
			// Repair birebir eşleşmede yüklemez: o zaman canlı olduğu gibi kalır.
			now := proxy.loaded
			if now == nil {
				now = proxy.live
			}
			if d := hosts(t, now)["example.com"]; len(d) != 1 || d[0] != "172.18.0.5:8080" {
				t.Errorf("canlı domain rotası korunmalı: %v", d)
			}
			if got := proxydrv.LiveRedirects(now, "example.com"); len(got) != 0 {
				t.Errorf("kaldırılan www yönlendirmesi taşındı: %v", got)
			}
			// Kaldırılan ad "uygulanamadı" sayılmaz: SkippedRedirects'e yazılsaydı
			// günlük, kasıtlı bir kaldırma için her turda uyarırdı.
			if len(skippedRedirects) != 0 {
				t.Errorf("kaldırılan ad SkippedRedirects'e yazıldı: %v", skippedRedirects)
			}
		})
	}
}

func TestReconcileCarriesOnlyStillDesiredRedirectsOfSkippedApp(t *testing.T) {
	desired := fakeDeployments{
		{AppID: "site", ReleaseID: "r2", Domain: "example.com", WWW: true, ContainerPort: 8080, Replicas: 1},
	}
	proxy, rc := skippedSite(t, []string{"www.example.com", "old.example.net"}, desired)

	if _, err := rc.Reconcile(context.Background()); err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if got := proxydrv.LiveRedirects(proxy.loaded, "example.com"); !reflect.DeepEqual(got, []string{"www.example.com"}) {
		t.Errorf("yönlendirmeler = %v, yalnız hâlâ istenen www.example.com bekleniyordu", got)
	}
	if _, err := proxydrv.SafeRoutes(proxy.loaded); err != nil {
		t.Errorf("aracı yüklenen kümeyi reddederdi: %v", err)
	}
}

// -domain ile taşınmış atlanan uygulama: canlı yönlendirme ESKİ domaine
// gidiyor ve o rota taşınmıyor. Ad hâlâ istense de taşınmamalı; taşınsaydı
// hedefi rotasız bir yönlendirme bütün kümeyi aracıda düşürürdü.
func TestReconcileDoesNotCarryRedirectToOldDomainOfSkippedApp(t *testing.T) {
	live := canli(t,
		proxydrv.AppRoute{
			AppID: "site", Domain: "old.example.com",
			Upstreams: []proxydrv.Upstream{{Dial: "172.18.0.5:8080"}},
			Redirects: []string{"keep.example.net"},
		},
		rota("api", "api.example.org", "172.18.0.6:8080"),
	)
	deps := fakeDeployments{
		{AppID: "api", ReleaseID: "r1", Domain: "api.example.org", ContainerPort: 8080, Replicas: 1},
		{AppID: "site", ReleaseID: "r2", Domain: "new.example.com", Redirects: []string{"keep.example.net"}, ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"api":  {running("api", "r1", 0, "172.18.0.6")},
		"site": {running("site", "r1", 0, "172.18.0.5")},
	}}
	proxy := &fakeProxy{live: live}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if res.Skipped["site"] == "" {
		t.Fatalf("site atlanmış olmalı: %v", res.Skipped)
	}
	if _, err := proxydrv.SafeRoutes(proxy.loaded); err != nil {
		t.Fatalf("aracı kümeyi reddederdi, bütün uygulamalar düşerdi: %v", err)
	}
	if d := hosts(t, proxy.loaded)["api.example.org"]; len(d) != 1 {
		t.Errorf("diğer uygulamanın rotası yüklenmedi: %v", d)
	}
	for _, to := range []string{"old.example.com", "new.example.com"} {
		if got := proxydrv.LiveRedirects(proxy.loaded, to); len(got) != 0 {
			t.Errorf("%s hedefli yönlendirme taşındı: %v", to, got)
		}
	}
}

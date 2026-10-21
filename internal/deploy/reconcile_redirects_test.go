package deploy

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/execclient"
	"github.com/erkanrzgc/kadran/internal/proxydrv"
	"github.com/erkanrzgc/kadran/internal/store"
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
				skipped, skippedRedirects = res.Skipped, res.SkippedRedirects
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

// Aracı MaxRedirects'i aşan kümeyi BÜTÜNÜYLE reddeder (her uygulama düşer):
// uzlaştırma sınırdan sonrasını ad ad atlamalı, kümeyi sınırda tutmalı.
func TestReconcileStopsAtTheBrokerRedirectLimit(t *testing.T) {
	names := make([]string, 0, proxydrv.MaxRedirects+1)
	for i := range proxydrv.MaxRedirects + 1 {
		names = append(names, fmt.Sprintf("r%04d.example.net", i))
	}
	last := names[len(names)-1]
	deps := fakeDeployments{
		{AppID: "big", ReleaseID: "r1", Domain: "example.com", Redirects: names, ContainerPort: 8080, Replicas: 1},
	}
	reps := fakeReplicas{byApp: map[string][]execclient.Replica{
		"big": {running("big", "r1", 0, "172.18.0.5")},
	}}
	proxy := &fakeProxy{}

	res, err := mustReconciler(t, deps, reps, proxy).Reconcile(context.Background())
	if err != nil {
		t.Fatalf("uzlaştırılamadı: %v", err)
	}
	if _, err := proxydrv.SafeRoutes(proxy.loaded); err != nil {
		t.Fatalf("aracı kümeyi reddederdi, bütün uygulamalar düşerdi: %v", err)
	}
	if got := proxydrv.LiveRedirects(proxy.loaded, "example.com"); len(got) != proxydrv.MaxRedirects {
		t.Errorf("%d yönlendirme yüklendi, %d bekleniyordu", len(got), proxydrv.MaxRedirects)
	}
	if want := map[string]string{last: "route set redirect limit reached"}; !reflect.DeepEqual(res.SkippedRedirects, want) {
		t.Errorf("SkippedRedirects = %v, %v bekleniyordu", res.SkippedRedirects, want)
	}
	if len(res.Skipped) != 0 {
		t.Errorf("sınır uygulamayı atlattı: %v", res.Skipped)
	}
}

// collidingNames: b'nin açık adı a'nın domaini; uzlaştırma yalnız o adı atlar.
func collidingNames() (fakeDeployments, fakeReplicas) {
	return fakeDeployments{
			{AppID: "a", ReleaseID: "r1", Domain: "a.example.com", ContainerPort: 8080, Replicas: 1},
			{AppID: "b", ReleaseID: "r1", Domain: "b.example.com", Redirects: []string{"a.example.com"}, ContainerPort: 8080, Replicas: 1},
		}, fakeReplicas{byApp: map[string][]execclient.Replica{
			"a": {running("a", "r1", 0, "172.18.0.5")},
			"b": {running("b", "r1", 0, "172.18.0.6")},
		}}
}

// Onarım da atlanan adları söylemeli: vekil izleyicisi onları günlüğe yazar.
func TestRepairReportsSkippedRedirects(t *testing.T) {
	deps, reps := collidingNames()
	res, err := mustReconciler(t, deps, reps, &fakeProxy{live: canli(t)}).Repair(context.Background())
	if err != nil {
		t.Fatalf("onarılamadı: %v", err)
	}
	if why := res.SkippedRedirects["a.example.com"]; !strings.Contains(why, `collides with the domain of app "a"`) {
		t.Errorf("SkippedRedirects = %v, a.example.com çakışması bekleniyordu", res.SkippedRedirects)
	}
}

// captureLog, testin süresince varsayılan slog'u bir tampona yönlendirir.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestWarnSkippedRedirectsLogsEachNameInOrder(t *testing.T) {
	buf := captureLog(t)
	WarnSkippedRedirects(map[string]string{"b.example.net": "no tls", "a.example.net": "invalid name"})

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("%d satır, 2 bekleniyordu:\n%s", len(lines), buf)
	}
	for i, want := range []string{`name=a.example.net reason="invalid name"`, `name=b.example.net reason="no tls"`} {
		if !strings.Contains(lines[i], "level=WARN") || !strings.Contains(lines[i], want) {
			t.Errorf("satır %d = %q, %q bekleniyordu", i, lines[i], want)
		}
	}
}

// Dağıtım ve iyileştirme de atlanan adları yutmamalı (yalnız app update
// söylüyordu). Geçersiz ad uzlaştırmada "invalid name" ile atlanır.
func TestRolloutLogsSkippedRedirects(t *testing.T) {
	withBadName := func(reps Replicas, proxy *fakeProxy) *Reconciler {
		rec, err := New(fakeDeployments{{AppID: testApp, ReleaseID: relNew, Domain: "example.test",
			Redirects: []string{"Bad.Example.Net"}, ContainerPort: 8080, Replicas: 1}}, reps, proxy, testAdmin())
		if err != nil {
			t.Fatalf("uzlaştırıcı kurulamadı: %v", err)
		}
		return rec
	}
	const want = `name=Bad.Example.Net reason="invalid name"`

	t.Run("dağıtım", func(t *testing.T) {
		buf := captureLog(t)
		h := newHarness(t, &fakeLifecycle{replicas: bothReleasesUp()}, true)
		h.rollout.rec = withBadName(fakeReplicas{byApp: map[string][]execclient.Replica{
			testApp: {running(testApp, relNew, 0, "172.20.0.3")}}}, h.proxy)
		if err := h.rollout.Run(context.Background(), testApplication(), store.Release{ID: relNew}); err != nil {
			t.Fatalf("dağıtım başarısız: %v", err)
		}
		if !strings.Contains(buf.String(), want) {
			t.Errorf("günlükte %q yok:\n%s", want, buf)
		}
	})
	t.Run("iyileştirme", func(t *testing.T) {
		buf := captureLog(t)
		w := &healWorld{replicas: []execclient.Replica{stoppedReplica(relNew, 0)}}
		h := newHealHarness(t, w)
		h.rollout.rec = withBadName(w, h.proxy)
		if _, err := h.rollout.Heal(context.Background(), h.app, h.rel); err != nil {
			t.Fatalf("iyileştirme başarısız: %v", err)
		}
		if !strings.Contains(buf.String(), want) {
			t.Errorf("günlükte %q yok:\n%s", want, buf)
		}
	})
}

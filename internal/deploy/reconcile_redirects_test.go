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

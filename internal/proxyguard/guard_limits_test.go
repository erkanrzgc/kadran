package proxyguard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

func postRoutes(t *testing.T, a *recordingAdmin, set proxydrv.RouteSet) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(set)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(string(body))))
	return w
}

func upstreams(n int) []proxydrv.ProxyUpstream {
	out := make([]proxydrv.ProxyUpstream, n)
	for i := range out {
		out[i] = proxydrv.ProxyUpstream{IP: fmt.Sprintf("172.18.0.%d", i+2), Port: 8080}
	}
	return out
}

func TestBrokerRejectsMoreThanMaxRoutes(t *testing.T) {
	routes := make([]proxydrv.ProxyRoute, proxydrv.MaxRoutes+1)
	for i := range routes {
		routes[i] = proxydrv.ProxyRoute{Domain: fmt.Sprintf("a%d.example.com", i), Upstreams: upstreams(1)}
	}
	a := &recordingAdmin{}
	if w := postRoutes(t, a, proxydrv.RouteSet{Routes: routes}); w.Code != http.StatusBadRequest {
		t.Errorf("%d routes accepted: %d", len(routes), w.Code)
	}
	if a.calls != 0 {
		t.Error("over-limit route set reached Caddy")
	}
}

func TestBrokerRejectsMoreThanMaxUpstreams(t *testing.T) {
	a := &recordingAdmin{}
	set := proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{Domain: "app.example.com", Upstreams: upstreams(proxydrv.MaxRouteUpstreams + 1)}}}
	if w := postRoutes(t, a, set); w.Code != http.StatusBadRequest {
		t.Errorf("%d upstreams accepted: %d", proxydrv.MaxRouteUpstreams+1, w.Code)
	}
	if a.calls != 0 {
		t.Error("over-limit upstream set reached Caddy")
	}
}

func TestBrokerRejectsDuplicateUpstream(t *testing.T) {
	a := &recordingAdmin{}
	up := proxydrv.ProxyUpstream{IP: "172.18.0.3", Port: 8080}
	set := proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{Domain: "app.example.com", Upstreams: []proxydrv.ProxyUpstream{up, up}}}}
	if w := postRoutes(t, a, set); w.Code != http.StatusBadRequest {
		t.Errorf("duplicate upstream accepted: %d", w.Code)
	}
	if a.calls != 0 {
		t.Error("duplicate upstream reached Caddy")
	}
}

func TestBrokerRejectsDockerGatewayAndNetworkAddresses(t *testing.T) {
	for _, ip := range []string{"172.18.0.1", "172.18.0.0", "172.18.255.255", "172.17.0.1"} {
		a := &recordingAdmin{}
		set := proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{Domain: "app.example.com", Upstreams: []proxydrv.ProxyUpstream{{IP: ip, Port: 8080}}}}}
		if w := postRoutes(t, a, set); w.Code != http.StatusBadRequest {
			t.Errorf("%s accepted: %d", ip, w.Code)
		}
		if a.calls != 0 {
			t.Errorf("%s reached Caddy", ip)
		}
	}
}

func TestBrokerRejectsNonDomainHosts(t *testing.T) {
	for _, host := range []string{"localhost", "172.18.0.3", "a..example.com", "-a.example.com"} {
		a := &recordingAdmin{}
		set := proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{Domain: host, Upstreams: upstreams(1)}}}
		if w := postRoutes(t, a, set); w.Code != http.StatusBadRequest {
			t.Errorf("host %q accepted: %d", host, w.Code)
		}
	}
}

func TestBrokerAllowsOnlyGetAndPost(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		a := &recordingAdmin{}
		w := httptest.NewRecorder()
		Handler(a).ServeHTTP(w, httptest.NewRequest(method, "/v1/routes", strings.NewReader(goodRoutes)))
		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", method, w.Code)
		}
		if a.calls != 0 {
			t.Errorf("%s reached Caddy", method)
		}
	}
}

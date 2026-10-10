package proxyguard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

type recordingAdmin struct {
	cfg   *proxydrv.Config
	live  *proxydrv.Config
	calls int
	err   error
}

func TestBrokerSupportsAll64AllowedReplicas(t *testing.T) {
	upstreams := make([]proxydrv.ProxyUpstream, 64)
	for i := range upstreams {
		upstreams[i] = proxydrv.ProxyUpstream{IP: fmt.Sprintf("172.18.0.%d", i+2), Port: 8080}
	}
	body, err := json.Marshal(proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{Domain: "app.example.com", Upstreams: upstreams}}})
	if err != nil {
		t.Fatal(err)
	}
	a := &recordingAdmin{}
	w := httptest.NewRecorder()
	Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(string(body))))
	if w.Code != http.StatusOK {
		t.Fatalf("valid 64 replica route rejected: %d %s", w.Code, w.Body.String())
	}
}

type blockedAdmin struct {
	recordingAdmin
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (a *blockedAdmin) Load(ctx context.Context, cfg *proxydrv.Config) error {
	if a.calls.Add(1) == 1 {
		close(a.started)
		<-a.release
	}
	return a.recordingAdmin.Load(ctx, cfg)
}

func TestCanceledQueuedProxyRequestCannotMutate(t *testing.T) {
	a := &blockedAdmin{started: make(chan struct{}), release: make(chan struct{})}
	handler := Handler(a)
	done := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(goodRoutes)))
		close(done)
	}()
	<-a.started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	second := make(chan struct{})
	go func() {
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(goodRoutes)).WithContext(ctx))
		close(second)
	}()
	select {
	case <-second:
	case <-time.After(100 * time.Millisecond):
	}
	close(a.release)
	<-done
	<-second
	if a.calls.Load() != 1 {
		t.Fatal("canceled queued request mutated Caddy")
	}
}

func (a *recordingAdmin) Load(_ context.Context, cfg *proxydrv.Config) error {
	a.calls++
	if a.err == nil {
		a.cfg = cfg
	}
	return a.err
}
func (a *recordingAdmin) Current(context.Context) (*proxydrv.Config, error) {
	if a.live != nil {
		return a.live, a.err
	}
	return a.cfg, a.err
}

const goodRoutes = `{"routes":[{"domain":"app.example.com","upstreams":[{"ip":"172.18.0.3","port":8080}]}]}`

func TestBrokerChecksFixedSettingsInLiveReadback(t *testing.T) {
	cfg, err := proxydrv.FixedConfig(proxydrv.RouteSet{Routes: []proxydrv.ProxyRoute{{Domain: "app.example.com", Upstreams: []proxydrv.ProxyUpstream{{IP: "172.18.0.3", Port: 8080}}}}})
	if err != nil {
		t.Fatal(err)
	}
	cfg.Admin.Listen = ":2019"
	a := &recordingAdmin{live: cfg}
	w := httptest.NewRecorder()
	Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(goodRoutes)))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("unsafe live settings returned success: %d %s", w.Code, w.Body.String())
	}
}

func TestBrokerAllowsOnlyTypedProxyRoute(t *testing.T) {
	a := &recordingAdmin{}
	w := httptest.NewRecorder()
	Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(goodRoutes)))
	if w.Code != http.StatusOK {
		t.Fatalf("good route: %d %s", w.Code, w.Body.String())
	}
	if a.calls != 1 || a.cfg.Admin.Listen != "fd/3" || a.cfg.Apps.HTTP.Servers["kadran"].Listen[0] != ":443" {
		t.Fatalf("fixed config not loaded: %+v", a)
	}
	b, _ := json.Marshal(a.cfg)
	if strings.Contains(string(b), "static_response") || strings.Contains(string(b), "{file.") {
		t.Fatalf("unsafe handler in %s", b)
	}
	w = httptest.NewRecorder()
	Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/routes", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ip":"172.18.0.3"`) || strings.Contains(w.Body.String(), `"admin"`) {
		t.Fatalf("typed readback: %d %s", w.Code, w.Body.String())
	}
}

func TestBrokerRejectsDangerousRequestsBeforeCaddy(t *testing.T) {
	cases := []string{
		`{"admin":{"listen":":2019"},"routes":[]}`,
		`{"routes":[],"apps":{"http":{"servers":{"leak":{"routes":[{"handle":[{"handler":"static_response","body":"{file./synthetic/key}"}]}]}}}}}`,
		`{"routes":[{"domain":"app.example.com","body":"{file./synthetic/key}","upstreams":[{"ip":"172.18.0.3","port":8080}]}]}`,
		strings.Replace(goodRoutes, "172.18.0.3", "unix//run/kadran-exec/exec.sock", 1),
		strings.Replace(goodRoutes, "172.18.0.3", "127.0.0.1", 1),
		strings.Replace(goodRoutes, "172.18.0.3", "::ffff:127.0.0.1", 1),
		strings.Replace(goodRoutes, "172.18.0.3", "169.254.169.254", 1),
		strings.Replace(goodRoutes, "172.18.0.3", "10.0.0.3", 1),
		strings.Replace(goodRoutes, "app.example.com", "{file./synthetic/key}", 1),
		strings.Replace(goodRoutes, "app.example.com", "*.example.com", 1),
		strings.Replace(goodRoutes, "app.example.com", "UPPER.example.com", 1),
		strings.Replace(goodRoutes, "8080", "0", 1),
		strings.Replace(goodRoutes, "8080", "65536", 1),
		`{"routes":[],"routes":[]}`,
		`{"routes":[]} {"routes":[]}`,
		`{}`, `null`, `{"routes":null}`,
	}
	for _, body := range cases {
		t.Run(body, func(t *testing.T) {
			a := &recordingAdmin{}
			w := httptest.NewRecorder()
			Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(body)))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status %d: %s", w.Code, w.Body.String())
			}
			if a.calls != 0 {
				t.Error("rejected request reached raw Caddy admin")
			}
		})
	}
}

func TestBrokerRejectsRawAdminPaths(t *testing.T) {
	a := &recordingAdmin{}
	for _, path := range []string{"/load", "/config/", "/stop", "/v1/routes?admin=1"} {
		w := httptest.NewRecorder()
		Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(goodRoutes)))
		if w.Code != http.StatusNotFound {
			t.Errorf("raw path %s accepted: %d", path, w.Code)
		}
	}
	if a.calls != 0 {
		t.Error("raw request reached Caddy")
	}
}

func TestBrokerBoundsBodyBeforeCaddy(t *testing.T) {
	a := &recordingAdmin{}
	w := httptest.NewRecorder()
	Handler(a).ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/v1/routes", strings.NewReader(strings.Repeat(" ", proxydrv.MaxRouteRequest+1))))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: %d", w.Code)
	}
	if a.calls != 0 {
		t.Error("oversized request reached Caddy")
	}
}

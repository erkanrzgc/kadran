package proxydrv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpstreamRejectsHostServices(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "169.254.169.254", "fe80::1", "ff02::1", "224.0.0.1", "fe80::1%eth0"} {
		if _, err := NewUpstream(ip, 8080); err == nil {
			t.Errorf("host service accepted: %s", ip)
		}
	}
}

func TestDaemonClientSendsOnlyTypedRoutes(t *testing.T) {
	var path string
	var body map[string]json.RawMessage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		_, _ = w.Write([]byte(`{"routes":[]}`))
	}))
	defer srv.Close()
	c := &Client{http: srv.Client()}
	// Direct HTTP is only this test's transport; production uses a Unix socket.
	c.http.Transport = &rewriteTransport{base: srv.Client().Transport, url: srv.URL}
	cfg, err := BuildConfig(BuildOptions{Admin: testAdmin()})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Load(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if path != "/v1/routes" {
		t.Errorf("daemon reached raw admin path %q", path)
	}
	if _, ok := body["admin"]; ok {
		t.Error("daemon sent Caddy admin configuration")
	}
	if _, ok := body["routes"]; !ok {
		t.Error("typed routes missing")
	}
}

type rewriteTransport struct {
	base http.RoundTripper
	url  string
}

func (t *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, t.url+r.URL.Path, r.Body)
	if err != nil {
		return nil, err
	}
	req.Header = r.Header
	return t.base.RoundTrip(req)
}

func TestDaemonClientVerifiesTypedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"routes":[]}`)) }))
	defer srv.Close()
	c := &Client{http: srv.Client()}
	c.http.Transport = &rewriteTransport{base: srv.Client().Transport, url: srv.URL}
	cfg, err := BuildConfig(BuildOptions{Admin: testAdmin(), Routes: []AppRoute{{AppID: "app", Domain: "app.example.com", Upstreams: []Upstream{{Dial: "172.18.0.3:8080"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Load(context.Background(), cfg); err == nil {
		t.Fatal("broker's missing route response counted as success")
	}
}

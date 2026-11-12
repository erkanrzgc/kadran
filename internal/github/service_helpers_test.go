package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	codeBody  = `{"device_code":"dc_SECRET123","user_code":"WDJB-MJHT","verification_uri":"https://github.com/login/device","expires_in":900,"interval":5}`
	tokenBody = `{"access_token":"ghu_new","expires_in":28800,"refresh_token":"ghr_new","refresh_token_expires_in":15897600,"token_type":"bearer"}`
	userBody  = `{"login":"octo","id":42}`
	pending   = `{"error":"authorization_pending"}`
)

// fakeClock testlerin saatidir; gerçek uyku yoktur.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// sleeper uyumayı sahte saati ilerleterek taklit eder ve süreleri kaydeder.
type sleeper struct {
	clock *fakeClock
	mu    sync.Mutex
	calls []time.Duration
}

func (s *sleeper) sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.calls = append(s.calls, d)
	s.mu.Unlock()
	s.clock.Advance(d)
	return nil
}

func (s *sleeper) durations() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]time.Duration(nil), s.calls...)
}

// fakeGH sahte GitHub'dır: yöntem+yol ile yönlendirir, istekleri kaydeder.
type fakeGH struct {
	mu     sync.Mutex
	routes map[string]http.HandlerFunc
	reqs   []string
	forms  map[string][]url.Values
	auths  map[string][]string
}

func newFakeGH() *fakeGH {
	return &fakeGH{routes: map[string]http.HandlerFunc{}, forms: map[string][]url.Values{}, auths: map[string][]string{}}
}

func (f *fakeGH) handle(method, path string, h http.HandlerFunc) { f.routes[method+" "+path] = h }

func (f *fakeGH) body(method, path, body string) {
	f.handle(method, path, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})
}

func (f *fakeGH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	f.reqs = append(f.reqs, r.Method+" "+r.URL.RequestURI())
	if r.Method == http.MethodPost {
		_ = r.ParseForm()
		f.forms[key] = append(f.forms[key], r.PostForm)
	}
	f.auths[key] = append(f.auths[key], r.Header.Get("Authorization"))
	h := f.routes[key]
	f.mu.Unlock()
	if h == nil {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

// count, kaydedilen isteklerden "METHOD /yol" önekiyle başlayanları sayar.
func (f *fakeGH) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if strings.HasPrefix(r, prefix) {
			n++
		}
	}
	return n
}

func (f *fakeGH) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reqs...)
}

func (f *fakeGH) postForms(path string) []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.forms["POST "+path]...)
}

func (f *fakeGH) authHeaders(method, path string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.auths[method+" "+path]...)
}

// seq art arda gelen gövdeleri döner; sonuncuyu tekrarlar.
func seq(bodies ...string) http.HandlerFunc {
	var i atomic.Int32
	return func(w http.ResponseWriter, _ *http.Request) {
		n := int(i.Add(1)) - 1
		if n >= len(bodies) {
			n = len(bodies) - 1
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(bodies[n]))
	}
}

type svcEnv struct {
	s     *Service
	gh    *fakeGH
	clock *fakeClock
	sl    *sleeper
	path  string
}

// newEnv Service kurar. connected true ise durum dosyası önceden yazılır.
// sleep nil ise sahte saati ilerleten varsayılan uyku kullanılır.
func newEnv(t *testing.T, gh *fakeGH, connected bool, sleep func(context.Context, time.Duration) error) *svcEnv {
	t.Helper()
	srv := httptest.NewServer(gh)
	t.Cleanup(srv.Close)
	clock := &fakeClock{t: tokenTestNow}
	sl := &sleeper{clock: clock}
	if sleep == nil {
		sleep = sl.sleep
	}
	path := filepath.Join(t.TempDir(), "state.json")
	if connected {
		if err := saveState(path, sampleState()); err != nil {
			t.Fatal(err)
		}
	}
	s, err := New(Config{
		ClientID: testClientID, AppSlug: "kadran-deploy", StateFile: path,
		Now: clock.Now, ep: endpoints{login: srv.URL, api: srv.URL}, sleep: sleep,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return &svcEnv{s: s, gh: gh, clock: clock, sl: sl, path: path}
}

// waitFlow geçerli yoklama goroutine'inin bitmesini bekler (kilitlenme koruması).
func (e *svcEnv) waitFlow(t *testing.T) {
	t.Helper()
	e.s.lifeMu.Lock()
	fr := e.s.flow
	e.s.lifeMu.Unlock()
	if fr == nil {
		return
	}
	select {
	case <-fr.done:
	case <-time.After(10 * time.Second):
		t.Fatal("device flow goroutine did not finish")
	}
}

func (e *svcEnv) status(t *testing.T) *kadranv1.GitHubConnection {
	t.Helper()
	c, err := e.s.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// flowGH cihaz akışının tüm uç noktalarını kurar: poll gövdeleri sırayla döner.
func flowGH(pollBodies ...string) *fakeGH {
	gh := newFakeGH()
	gh.body("POST", "/login/device/code", codeBody)
	gh.handle("POST", "/login/oauth/access_token", seq(pollBodies...))
	gh.body("GET", "/user", userBody)
	gh.body("GET", "/user/installations", `{"total_count":1,"installations":[{"id":7,"account":{"login":"octo"},"repository_selection":"selected"}]}`)
	gh.body("GET", "/user/installations/7/repositories", `{"total_count":2,"repositories":[{"full_name":"octo/priv","private":true},{"full_name":"octo/pub","private":false}]}`)
	return gh
}

// blockingSleep yoklama uykuya girince entered'a haber verir ve ctx bitene dek bekler.
func blockingSleep(entered chan<- struct{}) func(context.Context, time.Duration) error {
	return func(ctx context.Context, _ time.Duration) error {
		select {
		case entered <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return ctx.Err()
	}
}

// connectedGH bağlı durum için kapsam uç noktalarını kurar (cihaz akışı olmadan).
func connectedGH() *fakeGH {
	gh := flowGH(tokenBody)
	gh.body("GET", "/repos/octo/priv/tarball/"+testSHA, "TARDATA")
	return gh
}

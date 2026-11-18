package github

import (
	"context"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"google.golang.org/protobuf/encoding/prototext"
)

func TestDeviceFlowPendingThenSuccess(t *testing.T) {
	e := newEnv(t, flowGH(pending, tokenBody), false, nil)
	prompt, err := e.s.StartDeviceFlow(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if prompt.UserCode != "WDJB-MJHT" || prompt.VerificationUri != "https://github.com/login/device" ||
		prompt.InstallUrl != "https://github.com/apps/kadran-deploy/installations/new" ||
		!prompt.ExpiresAt.AsTime().Equal(tokenTestNow.Add(900*time.Second)) {
		t.Fatalf("prompt = %v", prompt)
	}
	e.waitFlow(t)
	if got := e.sl.durations(); len(got) != 2 || got[0] != 5*time.Second || got[1] != 5*time.Second {
		t.Fatalf("sleeps = %v, want [5s 5s]", got)
	}
	c := e.status(t)
	if c.State != kadranv1.GitHubConnection_STATE_CONNECTED || c.Login != "octo" || c.Pending != nil || !c.ConnectedAt.AsTime().Equal(tokenTestNow.Add(10*time.Second)) {
		t.Fatalf("status = %v", c)
	}
	st, ok, err := loadState(e.path, testClientID)
	if err != nil || !ok || st.AccessToken != "ghu_new" || st.RefreshToken != "ghr_new" || st.UserID != 42 || st.Login != "octo" {
		t.Fatalf("state = %+v ok %v err %v", st, ok, err)
	}
	if !st.AccessExpiresAt.Equal(tokenTestNow.Add(10*time.Second + 28800*time.Second)) {
		t.Fatalf("access expiry = %v", st.AccessExpiresAt)
	}
}

func TestStatusPendingShowsPromptWithoutDeviceCode(t *testing.T) {
	entered := make(chan struct{}, 1)
	e := newEnv(t, flowGH(pending), false, blockingSleep(entered))
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	c := e.status(t)
	if c.State != kadranv1.GitHubConnection_STATE_PENDING || c.Pending.GetUserCode() != "WDJB-MJHT" || c.InstallUrl == "" {
		t.Fatalf("status = %v", c)
	}
	if strings.Contains(prototext.Format(c), "dc_SECRET123") {
		t.Fatal("device code leaked into status")
	}
}

func TestSlowDownAddsFiveSeconds(t *testing.T) {
	e := newEnv(t, flowGH(`{"error":"slow_down"}`, tokenBody), false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if got := e.sl.durations(); len(got) != 2 || got[0] != 5*time.Second || got[1] != 10*time.Second {
		t.Fatalf("sleeps = %v, want [5s 10s]", got)
	}
}

func TestSlowDownUsesLargerServerInterval(t *testing.T) {
	e := newEnv(t, flowGH(`{"error":"slow_down","interval":20}`, tokenBody), false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if got := e.sl.durations(); len(got) != 2 || got[1] != 20*time.Second {
		t.Fatalf("sleeps = %v, want second 20s", got)
	}
}

// assertFlowFails poll yanıtındaki hata koduna göre FAILED + beklenen nedeni sınar.
func assertFlowFails(t *testing.T, body string, want kadranv1.GitHubFailure) {
	t.Helper()
	e := newEnv(t, flowGH(body), false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	c := e.status(t)
	if c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != want || c.Pending != nil {
		t.Fatalf("status = %v, want FAILED/%v", c, want)
	}
	if _, err := os.Stat(e.path); !os.IsNotExist(err) {
		t.Fatalf("state file must not exist after a failed flow: %v", err)
	}
	if strings.Contains(prototext.Format(c), "dc_SECRET123") {
		t.Fatal("device code leaked into status")
	}
}

func TestAccessDeniedFails(t *testing.T) {
	assertFlowFails(t, `{"error":"access_denied"}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_ACCESS_DENIED)
}

func TestExpiredTokenFails(t *testing.T) {
	assertFlowFails(t, `{"error":"expired_token"}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_CODE_EXPIRED)
}

func TestDeviceFlowDisabledFails(t *testing.T) {
	assertFlowFails(t, `{"error":"device_flow_disabled"}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED)
}

func TestOtherOAuthErrorsMapToBadClientOrInternal(t *testing.T) {
	assertFlowFails(t, `{"error":"incorrect_client_credentials"}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_BAD_CLIENT)
	assertFlowFails(t, `{"error":"unsupported_grant_type"}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_BAD_CLIENT)
	assertFlowFails(t, `{"error":"something_new"}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL)
	assertFlowFails(t, `{}`, kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL)
}

func TestLocalExpiryEndsFlowAsCodeExpired(t *testing.T) {
	// Sahte GitHub hep pending der; yerel saat 900 sn geçince akış biter.
	assertFlowFails(t, pending, kadranv1.GitHubFailure_GIT_HUB_FAILURE_CODE_EXPIRED)
}

func TestRateLimitedPollFails(t *testing.T) {
	gh := flowGH()
	gh.handle("POST", "/login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != kadranv1.GitHubFailure_GIT_HUB_FAILURE_RATE_LIMITED {
		t.Fatalf("status = %v", c)
	}
}

func TestTransientPollErrorsKeepPolling(t *testing.T) {
	gh := flowGH()
	var n atomic.Int32
	gh.handle("POST", "/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(tokenBody))
	})
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_CONNECTED {
		t.Fatalf("status = %v, want CONNECTED after a 502", c)
	}
}

func TestSaveFailureFailsFlowAsInternal(t *testing.T) {
	e := newEnv(t, flowGH(tokenBody), false, nil)
	old := saveStateFn
	saveStateFn = func(string, state) error { return os.ErrPermission }
	t.Cleanup(func() { saveStateFn = old })
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL || c.Login != "" {
		t.Fatalf("status = %v", c)
	}
}

func TestCodeRequestRejectionSetsFailure(t *testing.T) {
	gh := flowGH()
	gh.body("POST", "/login/device/code", `{"error":"device_flow_disabled"}`)
	e := newEnv(t, gh, false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err == nil {
		t.Fatal("rejected code request must return an error")
	}
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_FAILED || c.Failure != kadranv1.GitHubFailure_GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED {
		t.Fatalf("status = %v", c)
	}
}

func TestNewStartCancelsOldFlow(t *testing.T) {
	entered := make(chan struct{}, 1)
	e := newEnv(t, flowGH(pending), false, blockingSleep(entered))
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	e.s.lifeMu.Lock()
	first := e.s.flow
	e.s.lifeMu.Unlock()

	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.done:
	default:
		t.Fatal("old flow goroutine still running after a new Start")
	}
	e.s.lifeMu.Lock()
	second := e.s.flow
	e.s.lifeMu.Unlock()
	if second == first {
		t.Fatal("new Start must install a new flow")
	}
	if e.gh.count("POST /login/device/code") != 2 {
		t.Fatal("expected two device code requests")
	}
	// İptal edilen akış FAILED bırakmamalı; yeni akış PENDING.
	<-entered
	if c := e.status(t); c.State != kadranv1.GitHubConnection_STATE_PENDING {
		t.Fatalf("status = %v", c)
	}
}

func TestStartKeepsExistingConnectionUntilNewSucceeds(t *testing.T) {
	entered := make(chan struct{}, 1)
	e := newEnv(t, connectedGH(), true, blockingSleep(entered))
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-entered
	if c := e.status(t); c.Login != "octo" || c.State != kadranv1.GitHubConnection_STATE_PENDING {
		t.Fatalf("status = %v", c)
	}
	if d, err := e.s.Decide(context.Background(), "octo", "priv"); err != nil || d != DecisionArchive {
		t.Fatalf("Decide = %v, %v; old connection must keep working", d, err)
	}
}

func TestDeviceCodeRequestHasNoSecret(t *testing.T) {
	e := newEnv(t, flowGH(tokenBody), false, nil)
	if _, err := e.s.StartDeviceFlow(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitFlow(t)
	codes := e.gh.postForms("/login/device/code")
	if len(codes) != 1 || len(codes[0]) != 1 || codes[0].Get("client_id") != testClientID {
		t.Fatalf("device code form = %v, want only client_id", codes)
	}
	polls := e.gh.postForms("/login/oauth/access_token")
	if len(polls) != 1 || len(polls[0]) != 3 || polls[0].Get("device_code") != "dc_SECRET123" ||
		polls[0].Get("grant_type") != "urn:ietf:params:oauth:grant-type:device_code" || polls[0].Get("client_id") != testClientID {
		t.Fatalf("poll form = %v", polls)
	}
}

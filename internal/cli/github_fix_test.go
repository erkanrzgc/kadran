package cli

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

func TestConnectStopsWhenFirstPollIsNotConnected(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghState(kadranv1.GitHubConnection_STATE_NOT_CONNECTED)}}}
	c, _, errOut, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != ExitError {
		t.Fatalf("cikis %d", code)
	}
	if rpc.polls != 1 || !strings.Contains(errOut.String(), "the sign-in flow ended on the server; run connect again") {
		t.Errorf("polls=%d err=%s", rpc.polls, errOut)
	}
}

func TestConnectStopsOnUnknownState(t *testing.T) {
	for _, st := range []kadranv1.GitHubConnection_State{kadranv1.GitHubConnection_STATE_UNSPECIFIED, 99} {
		rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghState(st)}}}
		c, _, errOut, _ := ghCLI(rpc)
		if code := c.run(t.Context(), []string{"github", "connect"}); code != ExitError {
			t.Errorf("%v: cikis %d", st, code)
		}
		if rpc.polls != 1 || !strings.Contains(errOut.String(), "the sign-in flow ended on the server") {
			t.Errorf("%v: polls=%d err=%s", st, rpc.polls, errOut)
		}
	}
}

func TestConnectNilPromptFails(t *testing.T) {
	rpc := &fakeGitHubRPC{noPrompt: true}
	c, _, errOut, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != ExitError {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(errOut.String(), "the server returned no sign-in code") || rpc.polls != 0 {
		t.Errorf("polls=%d err=%s", rpc.polls, errOut)
	}
}

func TestConnectCtrlCDuringStart(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	rpc := &fakeGitHubRPC{startErr: status.Error(codes.Canceled, "context canceled"), onStart: cancel}
	c, _, errOut, _ := ghCLI(rpc)
	if code := c.run(ctx, []string{"github", "connect"}); code != exitInterrupted {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(errOut.String(), "the flow continues on the server until the code expires") ||
		strings.Contains(errOut.String(), "canceled") {
		t.Errorf("mesaj: %s", errOut)
	}
}

func TestGitHubStatusNoInstallationsAndNoConnectedAt(t *testing.T) {
	conn := ghState(kadranv1.GitHubConnection_STATE_CONNECTED)
	conn.Login = "octocat"
	c, out, _, _ := ghCLI(&fakeGitHubRPC{statusConn: conn})
	if code := c.run(t.Context(), []string{"github", "status"}); code != ExitOK {
		t.Fatalf("cikis %d", code)
	}
	text := out.String()
	for _, w := range []string{"No installation yet: install the app with the link below.", "since unknown"} {
		if !strings.Contains(text, w) {
			t.Errorf("%q yok:\n%s", w, text)
		}
	}
	if strings.Contains(text, "1970") || strings.Contains(text, "run `kadran github status`") {
		t.Errorf("istenmeyen metin:\n%s", text)
	}
	if n := strings.Count(text, ghInstallURL); n != 1 {
		t.Errorf("kurulum adresi %d kez:\n%s", n, text)
	}
}

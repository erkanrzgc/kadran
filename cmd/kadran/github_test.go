package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	ghInstallURL = "https://github.com/apps/kadran-deploy/installations/new"
	ghDeviceURL  = "https://github.com/login/device"
)

var ghBase = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)

// pollStep, GetGitHubStatus icin bir kayitli cevap: ya baglanti ya hata.
type pollStep struct {
	conn *kadranv1.GitHubConnection
	err  error
}

// fakeGitHubRPC, dort GitHub cagrisini cevaplar. Son adim sonsuza dek
// tekrarlanir.
type fakeGitHubRPC struct {
	kadranv1.KadranServiceClient
	startErr      error
	noPrompt      bool
	onStart       func()
	startCalls    int
	steps         []pollStep
	polls         int
	statusConn    *kadranv1.GitHubConnection
	statusErr     error
	disconnectErr error
	disconnected  int
}

func (f *fakeGitHubRPC) StartGitHubConnect(context.Context, *kadranv1.StartGitHubConnectRequest, ...grpc.CallOption) (*kadranv1.StartGitHubConnectResponse, error) {
	f.startCalls++
	if f.onStart != nil {
		f.onStart()
	}
	if f.noPrompt {
		return &kadranv1.StartGitHubConnectResponse{}, nil
	}
	if f.startErr != nil {
		return nil, f.startErr
	}
	return &kadranv1.StartGitHubConnectResponse{Prompt: &kadranv1.GitHubUserCodePrompt{
		UserCode: "ABCD-1234", VerificationUri: ghDeviceURL, InstallUrl: ghInstallURL,
		ExpiresAt: timestamppb.New(ghBase.Add(15 * time.Minute)),
	}}, nil
}

func (f *fakeGitHubRPC) GetGitHubStatus(context.Context, *kadranv1.GetGitHubStatusRequest, ...grpc.CallOption) (*kadranv1.GetGitHubStatusResponse, error) {
	if f.steps == nil {
		if f.statusErr != nil {
			return nil, f.statusErr
		}
		return &kadranv1.GetGitHubStatusResponse{Connection: f.statusConn}, nil
	}
	i := f.polls
	if i >= len(f.steps) {
		i = len(f.steps) - 1
	}
	f.polls++
	if f.steps[i].err != nil {
		return nil, f.steps[i].err
	}
	return &kadranv1.GetGitHubStatusResponse{Connection: f.steps[i].conn}, nil
}

func (f *fakeGitHubRPC) DisconnectGitHub(context.Context, *kadranv1.DisconnectGitHubRequest, ...grpc.CallOption) (*kadranv1.DisconnectGitHubResponse, error) {
	f.disconnected++
	if f.disconnectErr != nil {
		return nil, f.disconnectErr
	}
	return &kadranv1.DisconnectGitHubResponse{}, nil
}

// ghCLI, sahte saat ve uyku ile bir cli kurar: uyku saati ilerletir,
// gercekten beklemez. sleeps uyku sureleridir.
func ghCLI(rpc *fakeGitHubRPC) (*cli, *bytes.Buffer, *bytes.Buffer, *[]time.Duration) {
	c, out, errOut := newTestCLI("")
	c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
		return client.NewWithRPC(rpc), nil, nil
	}
	now := ghBase
	var sleeps []time.Duration
	c.now = func() time.Time { return now }
	c.sleep = func(_ context.Context, d time.Duration) error {
		sleeps = append(sleeps, d)
		now = now.Add(d)
		return nil
	}
	return c, out, errOut, &sleeps
}

func ghState(s kadranv1.GitHubConnection_State) *kadranv1.GitHubConnection {
	return &kadranv1.GitHubConnection{State: s, InstallUrl: ghInstallURL}
}

func ghConnected(insts ...*kadranv1.GitHubInstallation) *kadranv1.GitHubConnection {
	c := ghState(kadranv1.GitHubConnection_STATE_CONNECTED)
	c.Login = "octocat"
	c.ConnectedAt = timestamppb.New(ghBase)
	c.Installations = insts
	return c
}

func selectedInst(account string, n int, truncated bool) *kadranv1.GitHubInstallation {
	in := &kadranv1.GitHubInstallation{Account: account, Truncated: truncated}
	for range n {
		in.Repositories = append(in.Repositories, &kadranv1.GitHubRepository{FullName: account + "/r"})
	}
	return in
}

func TestGitHubConnectPrintsCodeAndWaits(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{
		{conn: ghState(kadranv1.GitHubConnection_STATE_PENDING)},
		{conn: ghState(kadranv1.GitHubConnection_STATE_PENDING)},
		{conn: ghConnected(selectedInst("octo-org", 3, false), &kadranv1.GitHubInstallation{Account: "octocat", AllRepositories: true})},
	}}
	c, out, errOut, sleeps := ghCLI(rpc)

	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitOK {
		t.Fatalf("cikis %d\n%s", code, errOut)
	}
	text := out.String()
	want := []string{
		"1. Install the Kadran Deploy app on the repositories Kadran may build:",
		ghInstallURL,
		"2. Open " + ghDeviceURL + " and enter the code:",
		"ABCD-1234",
		"Waiting for approval (expires " + ghBase.Add(15*time.Minute).Local().Format("15:04") + ")…",
		"Connected as octocat.",
		"octo-org: 3 repositories (selected)",
		"octocat: ALL repositories — consider Only select repositories",
	}
	last := -1
	for _, w := range want {
		i := strings.Index(text, w)
		if i < 0 {
			t.Fatalf("%q yok:\n%s", w, text)
		}
		if i < last {
			t.Errorf("%q sirasi yanlis:\n%s", w, text)
		}
		last = i
	}
	if len(*sleeps) != 2 || (*sleeps)[0] != 3*time.Second {
		t.Errorf("uykular = %v, 3 sn aralikla 2 bekleniyordu", *sleeps)
	}
}

func TestGitHubConnectTruncatedListGetsPlus(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghConnected(selectedInst("org", 100, true))}}}
	c, out, _, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(out.String(), "org: 100+ repositories (selected)") {
		t.Errorf("artisiz:\n%s", out)
	}
}

func TestGitHubConnectSingularRepository(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghConnected(selectedInst("org", 1, false))}}}
	c, out, _, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(out.String(), "org: 1 repository (selected)") {
		t.Errorf("tekil yok:\n%s", out)
	}
}

func TestConnectPrintsInstallLineWhenNoInstallation(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghConnected()}}}
	c, out, _, _ := ghCLI(rpc)

	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	for _, w := range []string{
		"Connected as octocat.",
		"No installation yet: install the app, then run `kadran github status`.",
	} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("%q yok:\n%s", w, out)
		}
	}
	if strings.Count(out.String(), ghInstallURL) < 2 {
		t.Errorf("kurulum adresi sonda tekrar basilmadi:\n%s", out)
	}
}

func TestConnectStopsWhenPendingFlowDisappears(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{
		{conn: ghState(kadranv1.GitHubConnection_STATE_PENDING)},
		{conn: ghState(kadranv1.GitHubConnection_STATE_NOT_CONNECTED)},
	}}
	c, _, errOut, _ := ghCLI(rpc)

	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitError {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(errOut.String(), "the sign-in flow ended on the server; run connect again") {
		t.Errorf("mesaj yok: %s", errOut)
	}
}

func TestConnectStopsAtExpiry(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghState(kadranv1.GitHubConnection_STATE_PENDING)}}}
	c, _, errOut, sleeps := ghCLI(rpc)

	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitError {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(errOut.String(), "the code expired: run kadran github connect again") {
		t.Errorf("mesaj yok: %s", errOut)
	}
	// 15 dk + 5 sn = 905 sn, 3 sn araliklarla: 302 uyku, sonsuz dongu yok.
	if n := len(*sleeps); n < 300 || n > 304 {
		t.Errorf("uyku sayisi = %d", n)
	}
}

func TestConnectFailureTexts(t *testing.T) {
	for failure, want := range map[kadranv1.GitHubFailure]string{
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_ACCESS_DENIED:        "access was denied on github.com",
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_CODE_EXPIRED:         "the code expired: run kadran github connect again",
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED: "device flow is disabled for the GitHub app",
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_BAD_CLIENT:           "the GitHub app client id was rejected",
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_NETWORK:              "kadran-git could not reach GitHub",
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_RATE_LIMITED:         "GitHub rate limit reached, retry later",
		kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL:             "connection failed",
	} {
		failed := ghState(kadranv1.GitHubConnection_STATE_FAILED)
		failed.Failure = failure
		c, _, errOut, _ := ghCLI(&fakeGitHubRPC{steps: []pollStep{{conn: failed}}})
		if code := c.run(t.Context(), []string{"github", "connect"}); code != exitError {
			t.Errorf("%v: cikis %d", failure, code)
		}
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("%v: %q yok: %s", failure, want, errOut)
		}
	}
}

func TestConnectTransientPollErrorRetries(t *testing.T) {
	unavail := status.Error(codes.Unavailable, "kadran-git is restarting")
	rpc := &fakeGitHubRPC{steps: []pollStep{
		{err: unavail},
		{conn: ghConnected(selectedInst("org", 1, false))},
	}}
	c, out, _, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(out.String(), "Connected as octocat.") {
		t.Errorf("baglanmadi:\n%s", out)
	}
}

func TestConnectStopsOnThirdConsecutiveUnavailable(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{err: status.Error(codes.Unavailable, "down")}}}
	c, _, errOut, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitError {
		t.Fatalf("cikis %d", code)
	}
	if rpc.polls != 3 {
		t.Errorf("yoklama = %d, 3 bekleniyordu", rpc.polls)
	}
	if !strings.Contains(errOut.String(), "down") {
		t.Errorf("sunucu metni yok: %s", errOut)
	}
}

func TestConnectStopsAtOnceOnFailedPrecondition(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{err: status.Error(codes.FailedPrecondition, "the kadran-git service is not running")}}}
	c, _, errOut, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitError {
		t.Fatalf("cikis %d", code)
	}
	if rpc.polls != 1 || !strings.Contains(errOut.String(), "the kadran-git service is not running") {
		t.Errorf("polls=%d err=%s", rpc.polls, errOut)
	}
}

func TestConnectCtrlCReportsServerContinues(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{conn: ghState(kadranv1.GitHubConnection_STATE_PENDING)}}}
	c, out, errOut, _ := ghCLI(rpc)
	ctx, cancel := context.WithCancel(t.Context())
	c.sleep = func(context.Context, time.Duration) error {
		cancel()
		return ctx.Err()
	}
	if code := c.run(ctx, []string{"github", "connect"}); code != exitInterrupted {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(out.String()+errOut.String(), "the flow continues on the server until the code expires") {
		t.Errorf("mesaj yok: %s %s", out, errOut)
	}
}

func TestGitHubOnOldServerSaysUpgrade(t *testing.T) {
	old := status.Error(codes.Unimplemented, "unknown method")
	for _, sub := range []string{"connect", "status", "disconnect"} {
		rpc := &fakeGitHubRPC{startErr: old, statusErr: old, disconnectErr: old}
		c, _, errOut, _ := ghCLI(rpc)
		if code := c.run(t.Context(), []string{"github", sub}); code != exitError {
			t.Errorf("%s: cikis %d", sub, code)
		}
		if !strings.Contains(errOut.String(), "upgrade the server") {
			t.Errorf("%s: upgrade yok: %s", sub, errOut)
		}
	}
}

func TestConnectOldServerAtPollSaysUpgrade(t *testing.T) {
	rpc := &fakeGitHubRPC{steps: []pollStep{{err: status.Error(codes.Unimplemented, "x")}}}
	c, _, errOut, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "connect"}); code != exitError {
		t.Fatalf("cikis %d", code)
	}
	if !strings.Contains(errOut.String(), "upgrade the server") {
		t.Errorf("upgrade yok: %s", errOut)
	}
}

func TestGitHubStatusWarnsAllRepositories(t *testing.T) {
	conn := ghConnected(&kadranv1.GitHubInstallation{Account: "octocat", AllRepositories: true}, selectedInst("org", 2, false))
	c, out, _, _ := ghCLI(&fakeGitHubRPC{statusConn: conn})

	if code := c.run(t.Context(), []string{"github", "status"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	for _, w := range []string{
		"octocat",
		"octocat: ALL repositories — consider Only select repositories",
		"org: 2 repositories (selected)",
		"Manage the app installation: " + ghInstallURL,
	} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("%q yok:\n%s", w, out)
		}
	}
	if strings.Contains(out.String(), "reconnect required") {
		t.Errorf("healthy connection told to reconnect:\n%s", out)
	}
}

// Bağlantı düşünce sunucu kimliği siler (dropForReconnect): gerçek şekil
// NOT_CONNECTED/FAILED/PENDING + reconnect_required, CONNECTED DEĞİL. Sebep
// satırı bu üç durumda da basılmalı; bayrak yokken basılmamalı.
func TestGitHubStatusShowsReconnectAfterDrop(t *testing.T) {
	const notice = "reconnect required: GitHub rejected the saved connection; run kadran github connect"
	pending := ghState(kadranv1.GitHubConnection_STATE_PENDING)
	pending.Pending = &kadranv1.GitHubUserCodePrompt{UserCode: "WXYZ-9999", ExpiresAt: timestamppb.New(ghBase)}
	failed := ghState(kadranv1.GitHubConnection_STATE_FAILED)
	failed.Failure = kadranv1.GitHubFailure_GIT_HUB_FAILURE_NETWORK
	for name, conn := range map[string]*kadranv1.GitHubConnection{
		"not connected": ghState(kadranv1.GitHubConnection_STATE_NOT_CONNECTED),
		"failed":        failed,
		"pending":       pending,
	} {
		conn.ReconnectRequired = true
		c, out, _, _ := ghCLI(&fakeGitHubRPC{statusConn: conn})
		if code := c.run(t.Context(), []string{"github", "status"}); code != exitOK {
			t.Errorf("%s: cikis %d", name, code)
		}
		if !strings.Contains(out.String(), notice) {
			t.Errorf("%s: reconnect notice missing:\n%s", name, out)
		}
	}
	c, out, _, _ := ghCLI(&fakeGitHubRPC{statusConn: ghState(kadranv1.GitHubConnection_STATE_NOT_CONNECTED)})
	if code := c.run(t.Context(), []string{"github", "status"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	if strings.Contains(out.String(), "reconnect required") {
		t.Errorf("never-connected server told to reconnect:\n%s", out)
	}
}

func TestGitHubStatusOtherStates(t *testing.T) {
	pending := ghState(kadranv1.GitHubConnection_STATE_PENDING)
	pending.Pending = &kadranv1.GitHubUserCodePrompt{UserCode: "WXYZ-9999", ExpiresAt: timestamppb.New(ghBase)}
	failed := ghState(kadranv1.GitHubConnection_STATE_FAILED)
	failed.Failure = kadranv1.GitHubFailure_GIT_HUB_FAILURE_ACCESS_DENIED
	for conn, want := range map[*kadranv1.GitHubConnection]string{
		ghState(kadranv1.GitHubConnection_STATE_NOT_CONNECTED): "not connected: run kadran github connect",
		pending: "WXYZ-9999",
		failed:  "access was denied on github.com",
	} {
		c, out, _, _ := ghCLI(&fakeGitHubRPC{statusConn: conn})
		if code := c.run(t.Context(), []string{"github", "status"}); code != exitOK {
			t.Errorf("cikis %d", code)
		}
		if !strings.Contains(out.String(), want) || !strings.Contains(out.String(), "Manage the app installation: "+ghInstallURL) {
			t.Errorf("%q ya da kurulum satiri yok:\n%s", want, out)
		}
	}
}

func TestGitHubDisconnectPrintsRevokeURLs(t *testing.T) {
	rpc := &fakeGitHubRPC{}
	c, out, _, _ := ghCLI(rpc)
	if code := c.run(t.Context(), []string{"github", "disconnect"}); code != exitOK {
		t.Fatalf("cikis %d", code)
	}
	for _, w := range []string{
		"Disconnected. The token was deleted on the server.",
		"https://github.com/settings/apps/authorizations",
		"https://github.com/settings/installations",
	} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("%q yok:\n%s", w, out)
		}
	}
	if rpc.disconnected != 1 {
		t.Errorf("DisconnectGitHub = %d kez", rpc.disconnected)
	}
}

func TestGitHubNeedsSubcommand(t *testing.T) {
	c, _, errOut, _ := ghCLI(&fakeGitHubRPC{})
	if code := c.run(t.Context(), []string{"github"}); code != exitUsage {
		t.Errorf("cikis %d", code)
	}
	if !strings.Contains(errOut.String(), "connect, status or disconnect") {
		t.Errorf("alt komutlar yok: %s", errOut)
	}
	if code := c.run(t.Context(), []string{"github", "zart"}); code != exitUsage {
		t.Errorf("cikis %d", code)
	}
}

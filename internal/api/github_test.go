package api

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/erkanrzgc/kadran/internal/connproto"
	"github.com/erkanrzgc/kadran/internal/gitclient"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	fakeUserCode = "WDJB-MJHT"
	fakeSHA      = "0123456789abcdef0123456789abcdef01234567"
)

// fakeGit, GitAdmin'in sahtesi. Alanlar mutekse korunur: eszamanli testler.
type fakeGit struct {
	mu       sync.Mutex
	prompt   *kadranv1.GitHubUserCodePrompt
	conn     *kadranv1.GitHubConnection
	sha      string
	err      error
	resolved [3]string
	resolveN int
	disconnN int
}

func (f *fakeGit) StartDeviceFlow(context.Context) (*kadranv1.GitHubUserCodePrompt, error) {
	return f.prompt, f.err
}

func (f *fakeGit) Status(context.Context) (*kadranv1.GitHubConnection, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.conn, f.err
}

func (f *fakeGit) Disconnect(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disconnN++
	return f.err
}

func (f *fakeGit) ResolveBranch(_ context.Context, owner, repo, branch string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = [3]string{owner, repo, branch}
	f.resolveN++
	return f.sha, f.err
}

func goodPrompt() *kadranv1.GitHubUserCodePrompt {
	return &kadranv1.GitHubUserCodePrompt{
		UserCode:        fakeUserCode,
		VerificationUri: "https://github.com/login/device",
		InstallUrl:      "https://github.com/apps/kadran-deploy/installations/new",
		ExpiresAt:       timestamppb.Now(),
	}
}

func connectedAt(t time.Time) *kadranv1.GitHubConnection {
	return &kadranv1.GitHubConnection{
		State:       kadranv1.GitHubConnection_STATE_CONNECTED,
		Login:       "erkanrzgc",
		ConnectedAt: timestamppb.New(t),
		InstallUrl:  "https://github.com/apps/kadran-deploy/installations/new",
		Installations: []*kadranv1.GitHubInstallation{
			{Account: "zeta", AllRepositories: true},
			{Account: "alfa", Repositories: []*kadranv1.GitHubRepository{{FullName: "alfa/a"}, {FullName: "alfa/b"}}},
		},
	}
}

func gitServer(t *testing.T, f *fakeGit) (*Server, func() []string) {
	t.Helper()
	srv, db := newTestServer(t)
	srv.git = f
	actions := func() []string {
		var out []string
		for _, r := range auditActions(t, db) {
			out = append(out, r.Action)
		}
		return out
	}
	return srv, actions
}

func countAction(list []string, name string) int {
	n := 0
	for _, a := range list {
		if a == name {
			n++
		}
	}
	return n
}

func TestDeployRoleCannotUseGitHubCalls(t *testing.T) {
	conn := yetkiIstemcisi(t, connproto.Identity{
		Origin: "ssh", Fingerprint: "SHA256:ci", Role: connproto.RoleDeploy, Apps: []string{"web"},
	})
	for _, m := range []string{"StartGitHubConnect", "GetGitHubStatus", "DisconnectGitHub", "ResolveGitHubBranch"} {
		if got := cagir(t, conn, m, nil); got != codes.PermissionDenied {
			t.Errorf("%s = %v, PermissionDenied bekleniyordu", m, got)
		}
	}
}

func TestGitHubCallsRequireTOTPSession(t *testing.T) {
	d := totpKur(t)
	d.etkinlestir()
	c := d.istemci(yonetici)
	ctx := context.Background()
	calls := map[string]func(context.Context) error{
		"StartGitHubConnect": func(x context.Context) error {
			_, err := c.StartGitHubConnect(x, &kadranv1.StartGitHubConnectRequest{})
			return err
		},
		"GetGitHubStatus": func(x context.Context) error {
			_, err := c.GetGitHubStatus(x, &kadranv1.GetGitHubStatusRequest{})
			return err
		},
		"DisconnectGitHub": func(x context.Context) error {
			_, err := c.DisconnectGitHub(x, &kadranv1.DisconnectGitHubRequest{})
			return err
		},
		"ResolveGitHubBranch": func(x context.Context) error {
			_, err := c.ResolveGitHubBranch(x, &kadranv1.ResolveGitHubBranchRequest{AppId: "a", Branch: "main"})
			return err
		},
	}
	for name, call := range calls {
		// Jetonsuz: oturum kapisi reddeder.
		err := call(ctx)
		if status.Code(err) != codes.Unauthenticated || !strings.Contains(err.Error(), "TOTP is on for this server") {
			t.Errorf("%s jetonsuz: %v", name, err)
		}
		// Kontrol grubu: gecerli jetonla kapi gecilir (kadran-git yok, o yuzden
		// FailedPrecondition; Unauthenticated DEGIL).
		err = call(oturumla(d.jeton))
		if status.Code(err) != codes.FailedPrecondition {
			t.Errorf("%s jetonlu: %v", name, err)
		}
	}
}

func TestGitHubDataWithControlOrBidiIsRejected(t *testing.T) {
	// Bidi override (U+202E) login icinde.
	c := connectedAt(time.Unix(1_800_000_000, 0))
	c.Login = "erkan\u202egpj"
	srv, actions := gitServer(t, &fakeGit{conn: c})
	if _, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); status.Code(err) != codes.Internal ||
		!strings.Contains(err.Error(), "unexpected data") {
		t.Fatalf("bidi login kabul edildi: %v", err)
	}
	if len(actions()) != 0 {
		t.Error("bidi login icin denetim yazildi")
	}

	// C1 CSI (U+009B) dogrulama adresinde.
	p := goodPrompt()
	p.VerificationUri = "https://github.com/login/\u009b31mdevice"
	srv2, actions2 := gitServer(t, &fakeGit{prompt: p})
	if _, err := srv2.StartGitHubConnect(context.Background(), &kadranv1.StartGitHubConnectRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("C1 adres kabul edildi: %v", err)
	}
	if len(actions2()) != 0 {
		t.Error("C1 adres icin denetim yazildi")
	}

	// Uzunluk siniri ve depo adi.
	c2 := connectedAt(time.Unix(1_800_000_000, 0))
	c2.Installations[0].Account = strings.Repeat("a", 257)
	c3 := connectedAt(time.Unix(1_800_000_000, 0))
	c3.Installations[1].Repositories[0].FullName = "a/b\u200b"
	for _, bad := range []*kadranv1.GitHubConnection{c2, c3} {
		srv3, _ := gitServer(t, &fakeGit{conn: bad})
		if _, err := srv3.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); status.Code(err) != codes.Internal {
			t.Errorf("gecersiz kurulum verisi kabul edildi: %v", err)
		}
	}
}

func TestConnectedIsAuditedOnceOnFirstSight(t *testing.T) {
	t1 := time.Unix(1_800_000_000, 5)
	f := &fakeGit{conn: connectedAt(t1)}
	srv, actions := gitServer(t, f)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if _, err := srv.GetGitHubStatus(ctx, &kadranv1.GetGitHubStatusRequest{}); err != nil {
			t.Fatal(err)
		}
	}
	if n := countAction(actions(), "github.connected"); n != 1 {
		t.Fatalf("github.connected = %d, 1 bekleniyordu", n)
	}

	f.mu.Lock()
	f.conn = connectedAt(t1.Add(time.Hour))
	f.mu.Unlock()
	if _, err := srv.GetGitHubStatus(ctx, &kadranv1.GetGitHubStatusRequest{}); err != nil {
		t.Fatal(err)
	}
	if n := countAction(actions(), "github.connected"); n != 2 {
		t.Fatalf("yeni connected_at sonrasi = %d, 2 bekleniyordu", n)
	}
}

func TestConnectedAuditParams(t *testing.T) {
	f := &fakeGit{conn: connectedAt(time.Unix(1_800_000_000, 0))}
	srv, db := newTestServer(t)
	srv.git = f
	if _, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); err != nil {
		t.Fatal(err)
	}
	recs := auditActions(t, db)
	if len(recs) != 1 {
		t.Fatalf("kayit sayisi = %d", len(recs))
	}
	p := string(recs[0].ParamsJSON)
	if !strings.Contains(p, "erkanrzgc") || !strings.Contains(p, "alfa:selected:2,zeta:all") {
		t.Errorf("params = %s", p)
	}
}

func TestConcurrentStatusAuditsOnce(t *testing.T) {
	f := &fakeGit{conn: connectedAt(time.Unix(1_800_000_000, 0))}
	srv, actions := gitServer(t, f)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if n := countAction(actions(), "github.connected"); n != 1 {
		t.Fatalf("github.connected = %d, 1 bekleniyordu", n)
	}
}

func TestAuditHasNoCodes(t *testing.T) {
	f := &fakeGit{prompt: goodPrompt(), conn: connectedAt(time.Unix(1_800_000_000, 0))}
	srv, db := newTestServer(t)
	srv.git = f
	ctx := context.Background()
	if _, err := srv.StartGitHubConnect(ctx, &kadranv1.StartGitHubConnectRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.GetGitHubStatus(ctx, &kadranv1.GetGitHubStatusRequest{}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DisconnectGitHub(ctx, &kadranv1.DisconnectGitHubRequest{}); err != nil {
		t.Fatal(err)
	}
	recs := auditActions(t, db)
	if len(recs) != 3 {
		t.Fatalf("kayit sayisi = %d, 3 bekleniyordu", len(recs))
	}
	for _, r := range recs {
		all := string(r.ParamsJSON) + r.Detail + r.Target
		for _, bad := range []string{fakeUserCode, "ghu_", "ghr_", "https://", "github.com/login"} {
			if strings.Contains(all, bad) {
				t.Errorf("%s kaydinda %q var: %s", r.Action, bad, all)
			}
		}
	}
	if f.disconnN != 1 {
		t.Errorf("Disconnect cagrisi = %d", f.disconnN)
	}
}

func TestStartRejectsForeignVerificationURI(t *testing.T) {
	for _, uri := range []string{"https://evil.example/login/device", "http://github.com/login/device",
		"https://github.com.evil.example/x", "", "https://github.com/a b"} {
		p := goodPrompt()
		p.VerificationUri = uri
		srv, actions := gitServer(t, &fakeGit{prompt: p})
		_, err := srv.StartGitHubConnect(context.Background(), &kadranv1.StartGitHubConnectRequest{})
		if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "unexpected data") {
			t.Errorf("%q: err = %v", uri, err)
		}
		if len(actions()) != 0 {
			t.Errorf("%q: denetim yazildi", uri)
		}
	}
	p := goodPrompt()
	p.InstallUrl = "https://github.com/login/device"
	srv, _ := gitServer(t, &fakeGit{prompt: p})
	if _, err := srv.StartGitHubConnect(context.Background(), &kadranv1.StartGitHubConnectRequest{}); status.Code(err) != codes.Internal {
		t.Errorf("yabanci install_url kabul edildi: %v", err)
	}
}

func TestStatusRejectsForeignInstallURL(t *testing.T) {
	c := connectedAt(time.Unix(1_800_000_000, 0))
	c.InstallUrl = "https://evil.example/apps/x"
	srv, actions := gitServer(t, &fakeGit{conn: c})
	if _, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("err = %v", err)
	}
	if len(actions()) != 0 {
		t.Error("gecersiz durumda denetim yazildi")
	}

	c2 := &kadranv1.GitHubConnection{
		State:   kadranv1.GitHubConnection_STATE_PENDING,
		Pending: &kadranv1.GitHubUserCodePrompt{UserCode: fakeUserCode, VerificationUri: "https://evil.example/"},
	}
	srv2, _ := gitServer(t, &fakeGit{conn: c2})
	if _, err := srv2.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); status.Code(err) != codes.Internal {
		t.Fatalf("pending uri kabul edildi: %v", err)
	}
}

func TestStatusRejectsBadPendingInstallURL(t *testing.T) {
	for _, u := range []string{"https://evil.example/apps/x", "https://github.com/apps/x\u009b31m", "https://github.com/apps/x\u202e"} {
		c := &kadranv1.GitHubConnection{
			State: kadranv1.GitHubConnection_STATE_PENDING,
			Pending: &kadranv1.GitHubUserCodePrompt{
				UserCode: fakeUserCode, VerificationUri: "https://github.com/login/device", InstallUrl: u,
			},
		}
		srv, actions := gitServer(t, &fakeGit{conn: c})
		_, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{})
		if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "kadran-git returned unexpected data") {
			t.Errorf("%q: err = %v", u, err)
		}
		if len(actions()) != 0 {
			t.Errorf("%q: denetim yazildi", u)
		}
	}
	// Kontrol grubu: gecerli pending.install_url kabul edilir.
	ok := &kadranv1.GitHubConnection{
		State: kadranv1.GitHubConnection_STATE_PENDING,
		Pending: &kadranv1.GitHubUserCodePrompt{
			UserCode: fakeUserCode, VerificationUri: "https://github.com/login/device",
			InstallUrl: "https://github.com/apps/kadran-deploy/installations/new",
		},
	}
	srv, _ := gitServer(t, &fakeGit{conn: ok})
	if _, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{}); err != nil {
		t.Errorf("gecerli pending reddedildi: %v", err)
	}
}

func TestResolveBranchUsesStoredRepo(t *testing.T) {
	f := &fakeGit{sha: fakeSHA}
	srv, _ := gitServer(t, f)
	mustCreateApp(t, srv, testSpec())

	resp, err := srv.ResolveGitHubBranch(context.Background(),
		&kadranv1.ResolveGitHubBranchRequest{AppId: "blog", Branch: "release/1"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetCommitSha() != fakeSHA {
		t.Errorf("sha = %q", resp.GetCommitSha())
	}
	if f.resolved != [3]string{"erkanrzgc", "kadran", "release/1"} {
		t.Errorf("sahteye giden = %v", f.resolved)
	}
}

func TestResolveBranchRejections(t *testing.T) {
	f := &fakeGit{sha: fakeSHA}
	srv, _ := gitServer(t, f)
	mustCreateApp(t, srv, testSpec())
	other := testSpec()
	other.AppId, other.GitHost = "gl", "gitlab.com"
	mustCreateApp(t, srv, other)
	ctx := context.Background()

	cases := []struct {
		app, branch string
		sha         string
		want        codes.Code
	}{
		{"yok", "main", fakeSHA, codes.NotFound},
		{"gl", "main", fakeSHA, codes.FailedPrecondition},
		{"blog", "-rf", fakeSHA, codes.InvalidArgument},
		{"blog", "main", "nothex", codes.Internal},
	}
	for _, c := range cases {
		f.sha = c.sha
		_, err := srv.ResolveGitHubBranch(ctx, &kadranv1.ResolveGitHubBranchRequest{AppId: c.app, Branch: c.branch})
		if status.Code(err) != c.want {
			t.Errorf("%s/%s: err = %v, %v bekleniyordu", c.app, c.branch, err, c.want)
		}
	}
	if f.resolveN != 1 {
		t.Errorf("kadran-git yalniz gecerli istekte cagrilmali, cagri = %d", f.resolveN)
	}
}

func TestGitHubUnavailableIsFailedPrecondition(t *testing.T) {
	srv, _ := newTestServer(t)
	c, err := gitclient.Dial(filepath.Join(t.TempDir(), "yok.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	srv.git = c

	_, err = srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{})
	if status.Code(err) != codes.FailedPrecondition ||
		!strings.Contains(err.Error(), "the kadran-git service is not running") {
		t.Fatalf("err = %v", err)
	}
}

func TestNilGitAdminIsFailedPrecondition(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := context.Background()
	errs := []error{}
	_, e := srv.StartGitHubConnect(ctx, &kadranv1.StartGitHubConnectRequest{})
	errs = append(errs, e)
	_, e = srv.GetGitHubStatus(ctx, &kadranv1.GetGitHubStatusRequest{})
	errs = append(errs, e)
	_, e = srv.DisconnectGitHub(ctx, &kadranv1.DisconnectGitHubRequest{})
	errs = append(errs, e)
	_, e = srv.ResolveGitHubBranch(ctx, &kadranv1.ResolveGitHubBranchRequest{AppId: "x", Branch: "main"})
	errs = append(errs, e)
	for i, err := range errs {
		if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "not running") {
			t.Errorf("cagri %d: err = %v", i, err)
		}
	}
}

// Baglanti dustugunde (GitHub kayitli jetonu reddetti) kadran-git kimligi
// siler: durum CONNECTED DEGIL, NOT_CONNECTED/FAILED/PENDING + reconnect_required.
// kadrand bu bayragi her durumda istemciye tasimali; yoksa CLI ve masaustu
// "yeniden baglan" uyarisini hic gosteremez (son inceleme I3).
func TestStatusKeepsReconnectRequiredWhenNotConnected(t *testing.T) {
	for _, st := range []kadranv1.GitHubConnection_State{
		kadranv1.GitHubConnection_STATE_NOT_CONNECTED,
		kadranv1.GitHubConnection_STATE_FAILED,
		kadranv1.GitHubConnection_STATE_PENDING,
	} {
		c := &kadranv1.GitHubConnection{
			State:             st,
			ReconnectRequired: true,
			InstallUrl:        "https://github.com/apps/kadran-deploy/installations/new",
		}
		if st == kadranv1.GitHubConnection_STATE_PENDING {
			c.Pending = goodPrompt()
		}
		srv, actions := gitServer(t, &fakeGit{conn: c})
		resp, err := srv.GetGitHubStatus(context.Background(), &kadranv1.GetGitHubStatusRequest{})
		if err != nil {
			t.Fatalf("%v: %v", st, err)
		}
		if got := resp.GetConnection(); got.GetState() != st || !got.GetReconnectRequired() {
			t.Fatalf("%v: response = %v, want the state and reconnect_required kept", st, got)
		}
		if len(actions()) != 0 {
			t.Errorf("%v: not-connected status was audited: %v", st, actions())
		}
	}
}

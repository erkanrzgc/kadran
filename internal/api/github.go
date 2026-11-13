package api

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/github"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/store"
)

// GitHub cihaz akisinin (K-167) dort cagrisi. Hepsi yalniz yoneticiye acik:
// deployAllowed listesine EKLENMEZ. TOTP kapisi CheckSession'dan kendiliginden
// gecer.

// settingGitHubConnectedAt, son denetlenen baglanti zaman damgasidir
// (RFC3339Nano, UTC).
const settingGitHubConnectedAt = "github.connected_at"

const (
	msgGitNotRunning  = "the kadran-git service is not running"
	msgGitBadAddress  = "kadran-git returned an unexpected address"
	githubVerifyPref  = "https://github.com/"
	githubInstallPref = "https://github.com/apps/"
)

// GitAdmin, kadrand'in kadran-git yonetim soketinden ihtiyac duydugu
// yuzeydir; *gitclient.Client karsilar. Arayuz KULLANILDIGI yerde tanimli.
type GitAdmin interface {
	StartDeviceFlow(ctx context.Context) (*kadranv1.GitHubUserCodePrompt, error)
	Status(ctx context.Context) (*kadranv1.GitHubConnection, error)
	Disconnect(ctx context.Context) error
	ResolveBranch(ctx context.Context, owner, repo, branch string) (string, error)
}

// gitAdmin, bagimlilik yoksa FailedPrecondition doner.
func (s *Server) gitAdmin() (GitAdmin, error) {
	if s.git == nil {
		return nil, status.Error(codes.FailedPrecondition, msgGitNotRunning)
	}
	return s.git, nil
}

// validGitHubURL, adresin beklenen oneke uydugunu ve bosluk/kontrol
// karakteri tasimadigini dogrular.
func validGitHubURL(u, prefix string) bool {
	if !strings.HasPrefix(u, prefix) {
		return false
	}
	for _, r := range u {
		if r <= ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

func validPrompt(p *kadranv1.GitHubUserCodePrompt) bool {
	return p != nil && validGitHubURL(p.GetVerificationUri(), githubVerifyPref) &&
		validGitHubURL(p.GetInstallUrl(), githubInstallPref)
}

func validConnection(c *kadranv1.GitHubConnection) bool {
	if c == nil {
		return false
	}
	if u := c.GetInstallUrl(); u != "" && !validGitHubURL(u, githubInstallPref) {
		return false
	}
	if p := c.GetPending(); p != nil && !validGitHubURL(p.GetVerificationUri(), githubVerifyPref) {
		return false
	}
	return true
}

// StartGitHubConnect, cihaz akisini baslatir.
func (s *Server) StartGitHubConnect(ctx context.Context, _ *kadranv1.StartGitHubConnectRequest) (*kadranv1.StartGitHubConnectResponse, error) {
	git, err := s.gitAdmin()
	if err != nil {
		return nil, err
	}
	prompt, err := git.StartDeviceFlow(ctx)
	if err != nil {
		return nil, err
	}
	if !validPrompt(prompt) {
		return nil, status.Error(codes.Internal, msgGitBadAddress)
	}
	// Parametre YOK: kod ve adres denetim kaydina girmez.
	if err := s.recordAction(ctx, "github.connect.start", "github", nil, audit.OutcomeSuccess, ""); err != nil {
		return nil, err
	}
	return &kadranv1.StartGitHubConnectResponse{Prompt: prompt}, nil
}

// GetGitHubStatus, baglanti durumunu doner; baglanti ilk kez
// gorulunce denetime yazar.
func (s *Server) GetGitHubStatus(ctx context.Context, _ *kadranv1.GetGitHubStatusRequest) (*kadranv1.GetGitHubStatusResponse, error) {
	git, err := s.gitAdmin()
	if err != nil {
		return nil, err
	}
	conn, err := git.Status(ctx)
	if err != nil {
		return nil, err
	}
	if !validConnection(conn) {
		return nil, status.Error(codes.Internal, msgGitBadAddress)
	}
	if err := s.auditFirstSight(ctx, conn); err != nil {
		return nil, err
	}
	return &kadranv1.GetGitHubStatusResponse{Connection: conn}, nil
}

// auditFirstSight, ayni connected_at icin tek kayit yazar. Karsilastir,
// denetle, sakla adimlari bir muteksle siralanir. Sira: ONCE denetim, SONRA
// saklama; ikisi arasinda cokus bir kez fazla kayit uretir, ters sira kaydi
// kaybederdi.
func (s *Server) auditFirstSight(ctx context.Context, conn *kadranv1.GitHubConnection) error {
	if conn.GetState() != kadranv1.GitHubConnection_STATE_CONNECTED || conn.GetConnectedAt() == nil {
		return nil
	}
	stamp := conn.GetConnectedAt().AsTime().UTC().Format(time.RFC3339Nano)

	s.githubMu.Lock()
	defer s.githubMu.Unlock()

	// Bagimsiz ctx: istemci kopsa da kayit/saklama yarim kalmasin.
	ctx = context.WithoutCancel(ctx)
	prev, ok, err := s.store.Setting(ctx, settingGitHubConnectedAt)
	if err != nil {
		return status.Errorf(codes.Internal, "could not read the GitHub setting: %v", err)
	}
	if ok && prev == stamp {
		return nil
	}
	params := map[string]string{
		"login":         conn.GetLogin(),
		"installations": installationsSummary(conn.GetInstallations()),
	}
	if err := s.recordAction(ctx, "github.connected", "github", params, audit.OutcomeSuccess, ""); err != nil {
		return err
	}
	if err := s.store.SetSetting(ctx, settingGitHubConnectedAt, stamp); err != nil {
		return status.Errorf(codes.Internal, "could not store the GitHub setting: %v", err)
	}
	return nil
}

// installationsSummary, "hesap:all" ya da "hesap:selected:N" listesidir.
func installationsSummary(list []*kadranv1.GitHubInstallation) string {
	parts := make([]string, 0, len(list))
	for _, in := range list {
		if in.GetAllRepositories() {
			parts = append(parts, in.GetAccount()+":all")
		} else {
			parts = append(parts, fmt.Sprintf("%s:selected:%d", in.GetAccount(), len(in.GetRepositories())))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// DisconnectGitHub, baglantiyi koparir. Saklanan connected_at yerinde
// kalir: sonraki baglanti farkli damga tasir ve denetlenir.
func (s *Server) DisconnectGitHub(ctx context.Context, _ *kadranv1.DisconnectGitHubRequest) (*kadranv1.DisconnectGitHubResponse, error) {
	git, err := s.gitAdmin()
	if err != nil {
		return nil, err
	}
	if err := git.Disconnect(ctx); err != nil {
		return nil, err
	}
	if err := s.recordAction(ctx, "github.disconnect", "github", nil, audit.OutcomeSuccess, ""); err != nil {
		return nil, err
	}
	return &kadranv1.DisconnectGitHubResponse{}, nil
}

// ResolveGitHubBranch, uygulamanin kayitli deposundaki dali commit
// karmasina cevirir. Okuma oldugu icin denetlenmez.
func (s *Server) ResolveGitHubBranch(ctx context.Context, req *kadranv1.ResolveGitHubBranchRequest) (*kadranv1.ResolveGitHubBranchResponse, error) {
	git, err := s.gitAdmin()
	if err != nil {
		return nil, err
	}
	app, err := s.store.GetApp(ctx, req.GetAppId())
	switch {
	case errors.Is(err, store.ErrAppNotFound):
		return nil, status.Error(codes.NotFound, "app not found")
	case err != nil:
		return nil, status.Errorf(codes.Internal, "could not read the app: %v", err)
	}
	if app.GitHost != "github.com" {
		return nil, status.Error(codes.FailedPrecondition,
			"branch resolution through the GitHub app works only for github.com apps")
	}
	if !github.ValidBranch(req.GetBranch()) {
		return nil, status.Error(codes.InvalidArgument, "invalid branch name")
	}
	sha, err := git.ResolveBranch(ctx, app.GitOwner, app.GitRepo, req.GetBranch())
	if err != nil {
		return nil, err
	}
	if !github.ValidSHA(sha) {
		return nil, status.Error(codes.Internal, "kadran-git returned an unexpected commit")
	}
	return &kadranv1.ResolveGitHubBranchResponse{CommitSha: sha}, nil
}

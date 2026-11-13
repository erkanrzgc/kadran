package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	// githubPollInterval, bağlanma sırasında durum yoklama aralığı.
	githubPollInterval = 3 * time.Second
	// githubExpirySlack, yerel saatin sunucudan geri kalabileceği pay:
	// kod bitiminden bu kadar sonra yerel olarak vazgeçilir.
	githubExpirySlack = 5 * time.Second
	// githubMaxPollErrors, art arda kaç Unavailable yoklama hatasında durulur.
	githubMaxPollErrors = 3

	// exitInterrupted, Ctrl-C ile bitiş (128 + SIGINT).
	exitInterrupted = 130

	msgGitHubUpgrade = "this server does not support GitHub connect: upgrade the server"
	msgGitHubExpired = "the code expired: run kadran github connect again"
	msgGitHubEnded   = "the sign-in flow ended on the server; run connect again"
	msgGitHubNoInst  = "No installation yet: install the app, then run `kadran github status`."
)

// runGitHub, `github` alt komutlarını dağıtır.
func (c *cli) runGitHub(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("`github` needs a subcommand: connect, status or disconnect")
	}
	switch args[0] {
	case "connect":
		return c.runGitHubWith(ctx, "github connect", args[1:], c.githubConnect)
	case "status":
		return c.runGitHubWith(ctx, "github status", args[1:], c.githubStatus)
	case "disconnect":
		return c.runGitHubWith(ctx, "github disconnect", args[1:], c.githubDisconnect)
	default:
		return c.usageError("unknown github subcommand %q — connect, status or disconnect", args[0])
	}
}

// runGitHubWith, üç alt komutun ortak kısmı: bayraklar, hedef, bağlantı.
// connect uzun sürebildiği için genel bir zaman sınırı koymaz; her çağrının
// kendi sınırı var.
func (c *cli) runGitHubWith(
	ctx context.Context, name string, args []string,
	run func(ctx context.Context, rpc kadranv1.KadranServiceClient) int,
) int {
	fs := c.newFlagSet(name)
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.argCountError("`%s` takes at most one target, got %d", name, fs.NArg())
	}
	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()
	return run(ctx, conn.RPC())
}

func (c *cli) githubConnect(ctx context.Context, rpc kadranv1.KadranServiceClient) int {
	callCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	resp, err := rpc.StartGitHubConnect(callCtx, &kadranv1.StartGitHubConnectRequest{})
	cancel()
	if err != nil {
		return c.githubFail("could not start the GitHub connection", err)
	}
	p := resp.GetPrompt()
	fmt.Fprintln(c.stdout, "1. Install the Kadran Deploy app on the repositories Kadran may build:")
	fmt.Fprintf(c.stdout, "   %s\n", p.GetInstallUrl())
	fmt.Fprintf(c.stdout, "2. Open %s and enter the code:\n", p.GetVerificationUri())
	fmt.Fprintf(c.stdout, "   %s\n", p.GetUserCode())
	expires := p.GetExpiresAt().AsTime()
	fmt.Fprintf(c.stdout, "Waiting for approval (expires %s)…\n", clockTime(expires))
	return c.pollGitHub(ctx, rpc, expires, p.GetInstallUrl())
}

// pollGitHub, bağlantı bitene kadar 3 sn arayla durumu yoklar.
func (c *cli) pollGitHub(ctx context.Context, rpc kadranv1.KadranServiceClient, expires time.Time, installURL string) int {
	var (
		failures    int
		sawPending  bool
		giveUpAfter = expires.Add(githubExpirySlack)
	)
	for {
		if c.clock().After(giveUpAfter) {
			return c.githubStop(msgGitHubExpired)
		}
		conn, err := c.githubStatusCall(ctx, rpc)
		switch {
		case ctx.Err() != nil:
			return c.githubInterrupted()
		case err != nil:
			failures++
			if status.Code(err) != codes.Unavailable || failures >= githubMaxPollErrors {
				return c.githubFail("could not get the GitHub status", err)
			}
		default:
			failures = 0
			if code, done := c.githubVerdict(conn, &sawPending, installURL); done {
				return code
			}
		}
		if c.sleepFor(ctx, githubPollInterval) != nil {
			return c.githubInterrupted()
		}
	}
}

// githubVerdict, bir yoklama cevabının akışı bitirip bitirmediğine karar verir.
func (c *cli) githubVerdict(conn *kadranv1.GitHubConnection, sawPending *bool, installURL string) (int, bool) {
	switch conn.GetState() {
	case kadranv1.GitHubConnection_STATE_CONNECTED:
		fmt.Fprintf(c.stdout, "Connected as %s.\n", conn.GetLogin())
		c.printInstallations(conn, installURL)
		return exitOK, true
	case kadranv1.GitHubConnection_STATE_FAILED:
		return c.githubStop(githubFailureText(conn.GetFailure())), true
	case kadranv1.GitHubConnection_STATE_PENDING:
		*sawPending = true
	case kadranv1.GitHubConnection_STATE_NOT_CONNECTED:
		if *sawPending {
			return c.githubStop(msgGitHubEnded), true
		}
	}
	return 0, false
}

func (c *cli) githubStatusCall(ctx context.Context, rpc kadranv1.KadranServiceClient) (*kadranv1.GitHubConnection, error) {
	callCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	resp, err := rpc.GetGitHubStatus(callCtx, &kadranv1.GetGitHubStatusRequest{})
	return resp.GetConnection(), err
}

func (c *cli) githubStatus(ctx context.Context, rpc kadranv1.KadranServiceClient) int {
	conn, err := c.githubStatusCall(ctx, rpc)
	if err != nil {
		return c.githubFail("could not get the GitHub status", err)
	}
	switch conn.GetState() {
	case kadranv1.GitHubConnection_STATE_CONNECTED:
		fmt.Fprintf(c.stdout, "connected as %s (since %s)\n", conn.GetLogin(), localTime(conn.GetConnectedAt().AsTime()))
		c.printInstallations(conn, conn.GetInstallUrl())
		if conn.GetReconnectRequired() {
			fmt.Fprintln(c.stdout, "reconnect required: run kadran github connect")
		}
	case kadranv1.GitHubConnection_STATE_PENDING:
		p := conn.GetPending()
		fmt.Fprintf(c.stdout, "waiting for approval: code %s, expires %s\n", p.GetUserCode(), clockTime(p.GetExpiresAt().AsTime()))
	case kadranv1.GitHubConnection_STATE_FAILED:
		fmt.Fprintf(c.stdout, "connection failed: %s\n", githubFailureText(conn.GetFailure()))
	default:
		fmt.Fprintln(c.stdout, "not connected: run kadran github connect")
	}
	if u := conn.GetInstallUrl(); u != "" {
		fmt.Fprintf(c.stdout, "Manage the app installation: %s\n", u)
	}
	return exitOK
}

func (c *cli) githubDisconnect(ctx context.Context, rpc kadranv1.KadranServiceClient) int {
	callCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	if _, err := rpc.DisconnectGitHub(callCtx, &kadranv1.DisconnectGitHubRequest{}); err != nil {
		return c.githubFail("could not disconnect GitHub", err)
	}
	fmt.Fprintln(c.stdout, "Disconnected. The token was deleted on the server.")
	fmt.Fprintln(c.stdout, "To revoke the grant on GitHub as well:")
	fmt.Fprintln(c.stdout, "  authorized apps:    https://github.com/settings/apps/authorizations")
	fmt.Fprintln(c.stdout, "  app installations:  https://github.com/settings/installations")
	return exitOK
}

// printInstallations, kurulum başına erişim satırını ve ALL uyarısını basar.
// Kurulum yoksa kurulum adresini söyler.
func (c *cli) printInstallations(conn *kadranv1.GitHubConnection, installURL string) {
	if len(conn.GetInstallations()) == 0 {
		fmt.Fprintln(c.stdout, msgGitHubNoInst)
		if installURL != "" {
			fmt.Fprintf(c.stdout, "   %s\n", installURL)
		}
		return
	}
	for _, in := range conn.GetInstallations() {
		if in.GetAllRepositories() {
			fmt.Fprintf(c.stdout, "  %s: ALL repositories — consider Only select repositories\n", in.GetAccount())
			continue
		}
		plus := ""
		if in.GetTruncated() {
			plus = "+"
		}
		fmt.Fprintf(c.stdout, "  %s: %d%s repositories (selected)\n", in.GetAccount(), len(in.GetRepositories()), plus)
	}
}

// githubFailureText, başarısızlık enum'unun kullanıcıya gösterilen metni.
func githubFailureText(f kadranv1.GitHubFailure) string {
	switch f {
	case kadranv1.GitHubFailure_GIT_HUB_FAILURE_ACCESS_DENIED:
		return "access was denied on github.com"
	case kadranv1.GitHubFailure_GIT_HUB_FAILURE_CODE_EXPIRED:
		return msgGitHubExpired
	case kadranv1.GitHubFailure_GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED:
		return "device flow is disabled for the GitHub app"
	case kadranv1.GitHubFailure_GIT_HUB_FAILURE_BAD_CLIENT:
		return "the GitHub app client id was rejected"
	case kadranv1.GitHubFailure_GIT_HUB_FAILURE_NETWORK:
		return "kadran-git could not reach GitHub"
	case kadranv1.GitHubFailure_GIT_HUB_FAILURE_RATE_LIMITED:
		return "GitHub rate limit reached, retry later"
	default:
		return "connection failed"
	}
}

// githubFail, bir RPC hatasını bildirir. Eski sunucu "upgrade the server"
// der; oturum hatası login ipucuyla gider; öteki gRPC hatalarında kadrand'ın
// sabit metni gösterilir.
func (c *cli) githubFail(what string, err error) int {
	switch {
	case status.Code(err) == codes.Unimplemented:
		return c.fail(errors.New(msgGitHubUpgrade))
	case client.IsSessionError(err):
		return c.fail(fmt.Errorf("%s: %w", what, err))
	case status.Code(err) != codes.Unknown:
		return c.fail(errors.New(status.Convert(err).Message()))
	default:
		return c.fail(fmt.Errorf("%s: %w", what, err))
	}
}

func (c *cli) githubStop(msg string) int {
	fmt.Fprintln(c.stderr, progName+":", msg)
	return exitError
}

func (c *cli) githubInterrupted() int {
	fmt.Fprintln(c.stderr, "the flow continues on the server until the code expires")
	return exitInterrupted
}

func (c *cli) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func clockTime(t time.Time) string { return t.Local().Format("15:04") }

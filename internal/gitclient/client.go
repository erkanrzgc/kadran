// Package gitclient, kadrand'in kadran-git yonetim soketine (admin.sock)
// baglanmasini saglar (GitHub cihaz akisi, K-167).
//
// kadran-git'in dondurdugu durum METNI asla iletilmez: yalniz gRPC kodu
// okunur ve metin kadrand'in kendi tablosundan gelir. Boylece kadran-git
// tarafindaki bir hata metni, oraya sizmis olabilecek her seyle birlikte
// kullaniciya ya da denetim kaydina ulasamaz.
package gitclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/backoff"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Cagri basina sure sinirlari.
const (
	shortTimeout   = 15 * time.Second
	resolveTimeout = 30 * time.Second
)

// MsgNotRunning, kadran-git'e ulasilamadiginda donen sabit metindir.
const MsgNotRunning = "the kadran-git service is not running"

// MsgGitHubFailed, kadran-git calisirken GitHub isteginin basarisiz oldugunu
// soyler (kadran-git'in genel hata kodu Unknown).
const MsgGitHubFailed = "GitHub request failed, retry later"

// Client, kadran-git yonetim soketine bagli bir gRPC istemcisidir.
type Client struct {
	conn *grpc.ClientConn
	rpc  kadranv1.GitAdminServiceClient
}

// Dial, yonetim soketine baglanir. Baglanti tembeldir: soket yoksa bile
// kadrand baslar, ilk cagri FailedPrecondition doner.
//
// Soketin erisimi soketin grubuyla degil, kadran-git in uid tabanli
// peercred politikasiyla (yalniz kadran uid i) zorlanir. Kurulumdaki
// "kadran, kadran-git grubunda degil" denetimi bunu destekler.
func Dial(socketPath string) (*Client, error) {
	conn, err := grpc.NewClient("unix:"+socketPath,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Varsayilan geri cekilme 120 sn ye cikar: kadran-git kalktiktan sonra
		// iki dakika "calismiyor" denirdi. Kisa tavan hizli toparlar.
		grpc.WithConnectParams(grpc.ConnectParams{
			Backoff: backoff.Config{
				BaseDelay: 500 * time.Millisecond, Multiplier: 1.6, Jitter: 0.2, MaxDelay: 5 * time.Second,
			},
			MinConnectTimeout: 5 * time.Second,
		}))
	if err != nil {
		return nil, fmt.Errorf("could not set up the kadran-git client: %w", err)
	}
	return &Client{conn: conn, rpc: kadranv1.NewGitAdminServiceClient(conn)}, nil
}

// Close, baglantiyi kapatir.
func (c *Client) Close() error { return c.conn.Close() }

// StartDeviceFlow, cihaz akisini baslatir.
func (c *Client) StartDeviceFlow(ctx context.Context) (*kadranv1.GitHubUserCodePrompt, error) {
	ctx, cancel := context.WithTimeout(ctx, shortTimeout)
	defer cancel()
	resp, err := c.rpc.StartDeviceFlow(ctx, &kadranv1.StartDeviceFlowRequest{})
	if err != nil {
		return nil, mapError(err)
	}
	return resp.GetPrompt(), nil
}

// Status, baglanti durumunu okur.
func (c *Client) Status(ctx context.Context) (*kadranv1.GitHubConnection, error) {
	ctx, cancel := context.WithTimeout(ctx, shortTimeout)
	defer cancel()
	resp, err := c.rpc.GetGitStatus(ctx, &kadranv1.GetGitStatusRequest{})
	if err != nil {
		return nil, mapError(err)
	}
	return resp.GetConnection(), nil
}

// Disconnect, baglantiyi koparir.
func (c *Client) Disconnect(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, shortTimeout)
	defer cancel()
	if _, err := c.rpc.DisconnectGit(ctx, &kadranv1.DisconnectGitRequest{}); err != nil {
		return mapError(err)
	}
	return nil
}

// ResolveBranch, dali commit karmasina cevirir.
func (c *Client) ResolveBranch(ctx context.Context, owner, repo, branch string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, resolveTimeout)
	defer cancel()
	resp, err := c.rpc.ResolveBranch(ctx, &kadranv1.ResolveBranchRequest{
		Owner: owner, Repo: repo, Branch: branch,
	})
	if err != nil {
		return "", mapError(err)
	}
	return resp.GetCommitSha(), nil
}

// mapError, kadran-git hatasini kadrand'in KENDI sabit metniyle eslar.
// Gelen durum mesajina hic bakilmaz.
//
// Unavailable yalniz gercek arama ya da tasima hatasidir (gRPC'nin kendi
// kodu): "calismiyor". kadran-git'in genel GitHub hatasi Unknown gelir ve
// "calismiyor" DEGILDIR; yeniden denenebilir bir Unavailable'a cevrilir.
func mapError(err error) error {
	st, ok := status.FromError(err)
	if !ok {
		return status.Error(codes.FailedPrecondition, MsgNotRunning)
	}
	switch st.Code() {
	case codes.Unavailable:
		return status.Error(codes.FailedPrecondition, MsgNotRunning)
	case codes.DeadlineExceeded:
		return status.Error(codes.Unavailable, "the kadran-git service did not answer")
	case codes.Unimplemented:
		return status.Error(codes.FailedPrecondition, "kadran-git is older than kadrand: upgrade the server")
	case codes.NotFound:
		return status.Error(codes.NotFound, "repository or branch not found (renamed or moved on GitHub? update the app's repository URL)")
	case codes.PermissionDenied:
		return status.Error(codes.PermissionDenied, "the GitHub app lacks access to this repository")
	case codes.FailedPrecondition:
		return status.Error(codes.FailedPrecondition, "GitHub connection needs attention: run kadran github status")
	case codes.ResourceExhausted:
		return status.Error(codes.ResourceExhausted, "GitHub rate limit reached, retry later")
	case codes.InvalidArgument:
		return status.Error(codes.InvalidArgument, "invalid repository or branch name")
	case codes.Canceled:
		return status.Error(codes.Canceled, "the request was canceled")
	case codes.Unknown:
		return status.Error(codes.Unavailable, MsgGitHubFailed)
	default:
		return status.Error(codes.Internal, "kadran-git failed")
	}
}

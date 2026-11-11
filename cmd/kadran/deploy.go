package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/erkanrzgc/kadran/internal/gitref"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// runDeploy, bir commit'i derler, ayağa kaldırır ve trafiği ona çevirir.
//
// ⚠ Yorumun eski hâli "bu dilimde dağıtım TRAFİK TAŞIMAZ" diyordu; dilim
// 4b'den beri YANLIŞ. Blue-green geçişi bağlandı: kapı → aktif sürüm →
// ters vekil → boşaltma. Ayrıntı internal/deploy/rollout.go'da.
func (c *cli) runDeploy(ctx context.Context, args []string) int {
	fs := c.newFlagSet("deploy")
	commit := fs.String("commit", "", "commit to build (full 40-character sha); if empty the branch is resolved")
	branch := fs.String("branch", "", "branch to resolve; if empty, the app's default branch")
	// ── ⚠ Varsayılan sınır YOK ve bu KASITLI ────────────────────────
	//
	// Diğer komutlar defaultTimeout (30 sn) kullanıyor. Derleme onlardan
	// farklı: dakikalarca sürebilir ve sabit bir sınır, tam da uzun
	// derlemeleri imkânsız kılar. Aynı hata bu projede bir kez ölçüldü —
	// 75 saniyelik bir derleme, 60 saniyelik bir istemci sınırı yüzünden
	// ölüyordu (docs/decisions.md K-044). Aynı tuzak burada, istemci
	// katmanında ÜÇÜNCÜ kez kurulabilirdi.
	//
	// Sınırsız bırakmak sorumsuzluk değil: bağlantı kurma aşamasının
	// kendi sınırı var (ssh ConnectTimeout=10 sn) ve komut SIGINT'e
	// duyarlı — Ctrl-C akışı iptal eder, sunucu tarafı sürümü mühürler.
	timeout := fs.Duration("timeout", 0, "overall time limit (0 = none; builds can take long)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.argCountError("usage: kadran deploy [-commit sha | -branch branch] " +
			"<app> [target] — options go BEFORE the app name")
	}
	appID, target := fs.Arg(0), fs.Arg(1)

	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	conn, _, err := c.connect(ctx, target)
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	sha, err := c.resolveCommit(ctx, conn.RPC(), appID, *commit, *branch)
	if err != nil {
		return c.fail(err)
	}

	stream, err := conn.RPC().Deploy(ctx, &kadranv1.DeployRequest{
		AppId:     appID,
		CommitSha: sha,
	})
	if err != nil {
		return c.fail(fmt.Errorf("could not start the deploy: %w", err))
	}
	return c.consumeDeploy(stream)
}

// consumeDeploy, dağıtım akışını tüketir ve çıkış kodunu belirler.
//
// ── Başarı ölçütü: DeploySucceeded GÖRÜLDÜ MÜ ──────────────────────
//
// "Akış hatasız bitti" YETMEZ. Sunucu başarıyı yalnızca son mesajla
// bildirir (K-042'nin bu katmandaki karşılığı); hata yokluğuna bakan bir
// istemci, sunucu tarafında sessizce değişen bir davranışı fark etmezdi.
// Bayrak burada, istemcide de ayrıca denetleniyor: iki uç aynı ölçütü
// kullanmazsa "başarılı" kelimesinin anlamı ikisinde ayrışır.
func (c *cli) consumeDeploy(stream kadranv1.KadranService_DeployClient) int {
	var (
		releaseID string
		succeeded *kadranv1.DeploySucceeded
	)

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if releaseID != "" {
				fmt.Fprintf(c.stderr, "\nRelease %s failed.\n", releaseID)
			}
			return c.fail(fmt.Errorf("deploy failed: %w", err))
		}

		switch {
		case msg.GetAccepted() != nil:
			acc := msg.GetAccepted()
			releaseID = acc.GetReleaseId()
			fmt.Fprintf(c.stderr, "Release %s · commit %s · build starting…\n\n",
				releaseID, shortSHA(acc.GetCommitSha()))

		case msg.GetOutput() != nil:
			// Derleyicinin İKİ akışı da stderr'e gider — stdout'a DEĞİL.
			//
			// Ayrım burada anlamlı değil: derleme çıktısının tamamı
			// ilerleme bilgisidir ve stdout borulandığında
			// (`kadran deploy … | jq`) makine okunabilir çıktıyı bozmamalı.
			// is_stderr bayrağı yine de taşınıyor; sürüm günlüğünü saklayan
			// bir tüketici (GUI) iki akışı ayrı renklendirebilsin.
			_, _ = c.stderr.Write(msg.GetOutput().GetData())

		case msg.GetSucceeded() != nil:
			succeeded = msg.GetSucceeded()
		}
	}

	if succeeded == nil {
		return c.fail(errors.New(
			"the deploy ended without reporting success — there is NO PROOF an image was built"))
	}

	fmt.Fprintf(c.stdout, "\nRelease %s is live · image %s\n",
		succeeded.GetReleaseId(), shortImage(succeeded.GetImageId()))
	return exitOK
}

// resolveCommit, dağıtılacak commit'i belirler; ilerleme satırı stderr'e
// gider. Kural ve gerekçe internal/gitref'te (masaüstü de onu kullanır).
func (c *cli) resolveCommit(
	ctx context.Context, rpc kadranv1.KadranServiceClient,
	appID, commit, branch string,
) (string, error) {
	return gitref.ResolveCommit(ctx, rpc, appID, commit, branch, c.stderr)
}

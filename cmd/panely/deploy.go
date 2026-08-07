package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

var fullSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)

// runDeploy, bir commit'i derler ve sürüm olarak kaydeder.
//
// ⚠ Bu dilimde dağıtım TRAFİK TAŞIMAZ: imaj üretilir ve sürüm kaydedilir,
// konteyner başlatılmaz. Blue-green geçişi dilim 4b'de.
func (c *cli) runDeploy(ctx context.Context, args []string) int {
	fs := c.newFlagSet("deploy")
	commit := fs.String("commit", "", "derlenecek commit (tam 40 haneli sha); boşsa dal çözülür")
	branch := fs.String("branch", "", "çözülecek dal; boşsa uygulamanın varsayılan dalı")
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
	timeout := fs.Duration("timeout", 0, "toplam süre sınırı (0 = sınırsız; derleme uzun sürebilir)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely deploy <uygulama> [hedef] [-commit sha | -branch dal]")
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

	stream, err := conn.RPC().Deploy(ctx, &panelyv1.DeployRequest{
		AppId:     appID,
		CommitSha: sha,
	})
	if err != nil {
		return c.fail(fmt.Errorf("dağıtım başlatılamadı: %w", err))
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
func (c *cli) consumeDeploy(stream panelyv1.PanelyService_DeployClient) int {
	var (
		releaseID string
		succeeded *panelyv1.DeploySucceeded
	)

	for {
		msg, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if releaseID != "" {
				fmt.Fprintf(c.stderr, "\nSürüm %s başarısız.\n", releaseID)
			}
			return c.fail(fmt.Errorf("dağıtım başarısız: %w", err))
		}

		switch {
		case msg.GetAccepted() != nil:
			acc := msg.GetAccepted()
			releaseID = acc.GetReleaseId()
			fmt.Fprintf(c.stderr, "Sürüm %s · commit %s · derleme başlıyor…\n\n",
				releaseID, shortSHA(acc.GetCommitSha()))

		case msg.GetOutput() != nil:
			// Derleyicinin İKİ akışı da stderr'e gider — stdout'a DEĞİL.
			//
			// Ayrım burada anlamlı değil: derleme çıktısının tamamı
			// ilerleme bilgisidir ve stdout borulandığında
			// (`panely deploy … | jq`) makine okunabilir çıktıyı bozmamalı.
			// is_stderr bayrağı yine de taşınıyor; sürüm günlüğünü saklayan
			// bir tüketici (GUI) iki akışı ayrı renklendirebilsin.
			_, _ = c.stderr.Write(msg.GetOutput().GetData())

		case msg.GetSucceeded() != nil:
			succeeded = msg.GetSucceeded()
		}
	}

	if succeeded == nil {
		return c.fail(errors.New(
			"dağıtım başarı bildirmeden bitti — imaj üretildiği KANITLANAMADI"))
	}

	fmt.Fprintf(c.stdout, "\nSürüm %s derlendi · imaj %s\n",
		succeeded.GetReleaseId(), shortImage(succeeded.GetImageId()))
	fmt.Fprintln(c.stderr,
		"Not: bu dilimde dağıtım trafik taşımaz; imaj üretildi ve sürüm kaydedildi.")
	return exitOK
}

// resolveCommit, dağıtılacak commit'i belirler.
//
// ── Neden çözümü İSTEMCİ yapıyor? ──────────────────────────────────
//
// panelyd yapamaz: systemd birimi `RestrictAddressFamilies=AF_UNIX` ile
// çalışıyor, yani TCP soketi AÇAMAZ. Kontrol düzlemi süreci ağa hiç
// çıkmıyor ve bu bir eksiklik değil, en-az-yetkinin ölçülebilir hâli.
// Dolayısıyla `git ls-remote` karşılığı burada, iş istasyonunda koşar.
func (c *cli) resolveCommit(
	ctx context.Context, rpc panelyv1.PanelyServiceClient,
	appID, commit, branch string,
) (string, error) {
	if commit != "" {
		if !fullSHA.MatchString(commit) {
			return "", fmt.Errorf(
				"-commit tam 40 haneli küçük harf onaltılık olmalı (%q) — "+
					"kısa sha ve dal adı kabul edilmez: derleme tekrarlanabilir olmalı", commit)
		}
		return commit, nil
	}

	// Dal çözümü için deponun nerede olduğunu bilmek gerekiyor; onu
	// sunucudaki tanımdan okuyoruz. Böylece kullanıcı depoyu iki kez
	// yazmıyor ve `app create`'te kaydedilen tanım tek gerçek kaynak
	// olarak kalıyor.
	resp, err := rpc.GetApp(ctx, &panelyv1.GetAppRequest{AppId: appID})
	if err != nil {
		return "", fmt.Errorf("uygulama tanımı alınamadı: %w", err)
	}
	spec := resp.GetApp().GetSpec()

	if branch == "" {
		branch = spec.GetGitBranch()
	}
	if branch == "" {
		return "", errors.New("dal belirlenemedi: -branch veya -commit verin")
	}

	fmt.Fprintf(c.stderr, "%s/%s/%s dalı %q çözülüyor…\n",
		spec.GetGitHost(), spec.GetGitOwner(), spec.GetGitRepo(), branch)

	sha, err := resolveRemoteBranch(ctx,
		spec.GetGitHost(), spec.GetGitOwner(), spec.GetGitRepo(), branch)
	if err != nil {
		return "", err
	}
	return sha, nil
}

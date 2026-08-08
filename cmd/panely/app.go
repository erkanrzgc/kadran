package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runApp, `app` alt komutlarını dağıtır.
func (c *cli) runApp(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("`app` bir alt komut ister: create, list veya show")
	}

	switch args[0] {
	case "create":
		return c.runAppCreate(ctx, args[1:])
	case "list":
		return c.runAppList(ctx, args[1:])
	case "show":
		return c.runAppShow(ctx, args[1:])
	default:
		return c.usageError("bilinmeyen app alt komutu %q — create, list veya show", args[0])
	}
}

// runAppCreate, yeni bir uygulama tanımı kaydeder.
func (c *cli) runAppCreate(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app create")
	repo := fs.String("repo", "", "kaynak depo: host/sahip/ad (ör. github.com/user/blog)")
	branch := fs.String("branch", "main", "varsayılan dal")
	dockerfile := fs.String("dockerfile", "", "depo köküne göreli Dockerfile yolu")
	domain := fs.String("domain", "", "yayınlanacak alan adı; boşsa uygulama yalnızca iç ağdan erişilir")
	port := fs.Uint("port", 8080, "uygulamanın konteyner içinde dinlediği port")
	replicas := fs.Uint("replicas", 1, "replika sayısı")
	health := fs.String("health-path", "/", "sağlık yoklaması yolu")
	memory := fs.String("memory", "512Mi", "bellek limiti (ör. 512Mi, 2Gi)")
	cpu := fs.Uint("cpu-millis", 1000, "CPU limiti, milli-çekirdek (1000 = 1 çekirdek)")
	blkio := fs.Uint("blkio-weight", 500, "blok G/Ç ağırlığı (10-1000)")
	buildArgs := c.stringMapFlag(fs, "build-arg", "derleme argümanı ANAHTAR=DEĞER (tekrarlanabilir)")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		// Seçeneklerin addan ÖNCE gelmesi Go'nun flag paketinin kuralı:
		// ilk konumsal argümanda ayrıştırma durur. Kullanım metni bunu
		// açıkça söylüyor, çünkü tersini deneyen biri "-repo zorunlu"
		// hatası alır ve sebebini göremez.
		return c.usageError("kullanım: panely app create -repo host/sahip/depo " +
			"[seçenekler] <ad> [hedef] — seçenekler addan ÖNCE gelir")
	}

	host, owner, name, err := splitRepo(*repo)
	if err != nil {
		return c.usageError("%v", err)
	}
	mem, err := parseSize(*memory)
	if err != nil {
		return c.usageError("%v", err)
	}

	spec := &panelyv1.AppSpec{
		AppId:          fs.Arg(0),
		GitHost:        host,
		GitOwner:       owner,
		GitRepo:        name,
		GitBranch:      *branch,
		DockerfilePath: *dockerfile,
		BuildArgs:      *buildArgs,
		ContainerPort:  uint32(*port),     //nolint:gosec // sunucu 1-65535 doğruluyor
		Replicas:       uint32(*replicas), //nolint:gosec // sunucu 1-64 doğruluyor
		HealthPath:     *health,
		Domain:         *domain,
		Limits: &panelyv1.ResourceLimits{
			MemoryBytes: mem,
			CpuMillis:   uint32(*cpu),   //nolint:gosec // sunucu doğruluyor
			BlkioWeight: uint32(*blkio), //nolint:gosec // sunucu 10-1000 doğruluyor
		},
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().CreateApp(ctx, &panelyv1.CreateAppRequest{Spec: spec})
	if err != nil {
		return c.fail(fmt.Errorf("uygulama oluşturulamadı: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	s := resp.GetApp().GetSpec()
	fmt.Fprintf(c.stdout, "Uygulama oluşturuldu: %s\n", s.GetAppId())
	fmt.Fprintf(c.stdout, "  Kaynak  : %s/%s/%s (%s)\n",
		s.GetGitHost(), s.GetGitOwner(), s.GetGitRepo(), s.GetGitBranch())
	fmt.Fprintf(c.stdout, "  Port    : %d · replika: %d\n", s.GetContainerPort(), s.GetReplicas())
	fmt.Fprintf(c.stdout, "\nDağıtmak için: panely deploy %s\n", s.GetAppId())
	return exitOK
}

func (c *cli) runAppList(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app list")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`app list` en fazla bir hedef alır, %d verildi", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListApps(ctx, &panelyv1.ListAppsRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("uygulamalar alınamadı: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	if len(resp.GetApps()) == 0 {
		fmt.Fprintln(c.stdout, "Tanımlı uygulama yok. `panely app create` ile başlayın.")
		return exitOK
	}

	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "AD\tKAYNAK\tDAL\tPORT\tREPLİKA\tSÜRÜM")
	for _, a := range resp.GetApps() {
		s := a.GetSpec()
		fmt.Fprintf(tw, "%s\t%s/%s/%s\t%s\t%d\t%d\t%d\n",
			s.GetAppId(), s.GetGitHost(), s.GetGitOwner(), s.GetGitRepo(),
			s.GetGitBranch(), s.GetContainerPort(), s.GetReplicas(), a.GetReleaseCount())
	}
	_ = tw.Flush()
	return exitOK
}

func (c *cli) runAppShow(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app show")
	limit := fs.Uint("releases", 10, "gösterilecek sürüm sayısı")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely app show [seçenekler] <ad> [hedef]")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().GetApp(ctx, &panelyv1.GetAppRequest{
		AppId:        fs.Arg(0),
		ReleaseLimit: uint32(*limit), //nolint:gosec // sunucu üst sınırı uyguluyor
	})
	if err != nil {
		return c.fail(fmt.Errorf("uygulama alınamadı: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	c.printApp(resp)
	return exitOK
}

func (c *cli) printApp(resp *panelyv1.GetAppResponse) {
	s := resp.GetApp().GetSpec()
	fmt.Fprintf(c.stdout, "%s\n", s.GetAppId())
	fmt.Fprintf(c.stdout, "  Kaynak   : %s/%s/%s (%s)\n",
		s.GetGitHost(), s.GetGitOwner(), s.GetGitRepo(), s.GetGitBranch())
	if d := s.GetDomain(); d != "" {
		fmt.Fprintf(c.stdout, "  Alan adı : %s\n", d)
	}
	fmt.Fprintf(c.stdout, "  Port     : %d · replika: %d · sağlık: %s\n",
		s.GetContainerPort(), s.GetReplicas(), s.GetHealthPath())
	if l := s.GetLimits(); l != nil {
		fmt.Fprintf(c.stdout, "  Limitler : %s bellek · %d milli-cpu · blkio %d\n",
			formatSize(l.GetMemoryBytes()), l.GetCpuMillis(), l.GetBlkioWeight())
	}

	releases := resp.GetReleases()
	if len(releases) == 0 {
		fmt.Fprintf(c.stdout, "\nHenüz sürüm yok. `panely deploy %s` ile derleyin.\n", s.GetAppId())
		return
	}

	fmt.Fprintln(c.stdout, "\nSürümler:")
	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  SÜRÜM\tCOMMIT\tDURUM\tİMAJ\tBAŞLANGIÇ")
	for _, r := range releases {
		started := "-"
		if ts := r.GetStartedAt(); ts != nil {
			started = ts.AsTime().Local().Format("2006-01-02 15:04:05")
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n",
			r.GetReleaseId(), shortSHA(r.GetCommitSha()),
			releaseStatusLabel(r.GetStatus()), shortImage(r.GetImageId()), started)
	}
	_ = tw.Flush()

	// Başarısız sürümlerin sebebi ayrı basılır: tabloya sığmaz ve asıl
	// aranan bilgi odur.
	for _, r := range releases {
		if r.GetStatus() == panelyv1.ReleaseStatus_RELEASE_STATUS_FAILED && r.GetDetail() != "" {
			fmt.Fprintf(c.stdout, "\n%s başarısız: %s\n", r.GetReleaseId(), r.GetDetail())
		}
	}
}

func releaseStatusLabel(s panelyv1.ReleaseStatus) string {
	switch s {
	case panelyv1.ReleaseStatus_RELEASE_STATUS_BUILDING:
		return "derleniyor"
	case panelyv1.ReleaseStatus_RELEASE_STATUS_BUILT:
		return "derlendi"
	case panelyv1.ReleaseStatus_RELEASE_STATUS_FAILED:
		return "başarısız"
	default:
		return "bilinmiyor"
	}
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// shortImage, "sha256:" önekini atıp ilk 12 haneyi gösterir.
func shortImage(id string) string {
	if id == "" {
		return "-"
	}
	return shortSHA(strings.TrimPrefix(id, "sha256:"))
}

// splitRepo, "host/sahip/ad" üçlüsünü ayırır.
//
// Şema KABUL EDİLMEZ. "https://github.com/user/repo" yazan biri aslında
// bir URL veriyor ve bu tasarımda hiçbir yerde URL ALINMIYOR: bağlam
// URL'ini executor kendisi kuruyor. Sessizce kırpmak, o sınırı bulanık
// gösterirdi.
func splitRepo(s string) (host, owner, name string, err error) {
	if s == "" {
		return "", "", "", fmt.Errorf("-repo zorunlu (ör. github.com/kullanici/depo)")
	}
	if strings.Contains(s, "://") {
		return "", "", "", fmt.Errorf(
			"-repo bir URL değil, host/sahip/ad üçlüsüdür (%q) — "+
				"şema ve yol executor tarafında kurulur", s)
	}
	parts := strings.Split(strings.TrimSuffix(s, ".git"), "/")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf(
			"-repo host/sahip/ad biçiminde olmalı (%q)", s)
	}
	return parts[0], parts[1], parts[2], nil
}

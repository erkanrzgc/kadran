package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// runApp, `app` alt komutlarını dağıtır.
func (c *cli) runApp(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("`app` needs a subcommand: create, update, list, show or delete")
	}

	switch args[0] {
	case "create":
		return c.runAppCreate(ctx, args[1:])
	case "update":
		return c.runAppUpdate(ctx, args[1:])
	case "list":
		return c.runAppList(ctx, args[1:])
	case "show":
		return c.runAppShow(ctx, args[1:])
	case "delete":
		return c.runAppDelete(ctx, args[1:])
	default:
		return c.usageError(
			"unknown app subcommand %q — create, update, list, show or delete", args[0])
	}
}

// runAppCreate, yeni bir uygulama tanımı kaydeder.
func (c *cli) runAppCreate(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app create")
	repo := fs.String("repo", "", "source repository: host/owner/name (e.g. github.com/user/blog)")
	branch := fs.String("branch", "main", "default branch")
	dockerfile := fs.String("dockerfile", "", "Dockerfile path relative to the repository root")
	domain := fs.String("domain", "", "domain to serve the app on; if empty the app is reachable only from the internal network")
	skipDNS := fs.Bool(skipDNSCheckFlag, false, "continue even if the domain's DNS does not point at the server (e.g. Cloudflare proxy)")
	port := fs.Uint("port", 8080, "port the app listens on inside the container")
	replicas := fs.Uint("replicas", 1, "number of replicas")
	health := fs.String("health-path", "/", "health check path")
	memory := fs.String("memory", "512Mi", "memory limit (e.g. 512Mi, 2Gi)")
	cpu := fs.Uint("cpu-millis", 1000, "CPU limit in millicores (1000 = 1 core)")
	blkio := fs.Uint("blkio-weight", 500, "block I/O weight (10-1000)")
	buildArgs := c.stringMapFlag(fs, "build-arg", "build argument KEY=VALUE (repeatable)")
	env := c.stringMapFlag(fs, "env", "environment variable KEY=VALUE (repeatable); sealed at rest, but the running container gets it as a plain variable that docker inspect shows")
	volumes := c.volumeFlag(fs, "volume",
		"persistent volume NAME:/mount/point[:ro] (repeatable)")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		// Seçeneklerin addan ÖNCE gelmesi Go'nun flag paketinin kuralı:
		// ilk konumsal argümanda ayrıştırma durur. Kullanım metni bunu
		// açıkça söylüyor, çünkü tersini deneyen biri "-repo zorunlu"
		// hatası alır ve sebebini göremez.
		return c.usageError("usage: kadran app create -repo host/owner/repo " +
			"[options] <name> [target] — options go BEFORE the name")
	}

	host, owner, name, err := splitRepo(*repo)
	if err != nil {
		return c.usageError("%v", err)
	}
	mem, err := parseSize(*memory)
	if err != nil {
		return c.usageError("%v", err)
	}

	spec := &kadranv1.AppSpec{
		AppId:          fs.Arg(0),
		GitHost:        host,
		GitOwner:       owner,
		GitRepo:        name,
		GitBranch:      *branch,
		DockerfilePath: *dockerfile,
		BuildArgs:      *buildArgs,
		Env:            *env,
		Volumes:        *volumes,
		ContainerPort:  uint32(*port),     //nolint:gosec // sunucu 1-65535 doğruluyor
		Replicas:       uint32(*replicas), //nolint:gosec // sunucu 1-64 doğruluyor
		HealthPath:     *health,
		Domain:         *domain,
		Limits: &kadranv1.ResourceLimits{
			MemoryBytes: mem,
			CpuMillis:   uint32(*cpu),   //nolint:gosec // sunucu doğruluyor
			BlkioWeight: uint32(*blkio), //nolint:gosec // sunucu 10-1000 doğruluyor
		},
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	// Bağlanmadan ÖNCE: yanlış DNS'le kaydedilen alan adı sertifika
	// alamıyordu ve bunu söyleyen bir şey yoktu (K-128).
	if err := c.checkDomain(ctx, *domain, fs.Arg(1), *skipDNS); err != nil {
		return c.fail(err)
	}

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().CreateApp(ctx, &kadranv1.CreateAppRequest{Spec: spec})
	if err != nil {
		return c.fail(fmt.Errorf("could not create the app: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	s := resp.GetApp().GetSpec()
	fmt.Fprintf(c.stdout, "App created: %s\n", s.GetAppId())
	fmt.Fprintf(c.stdout, "  Source  : %s/%s/%s (%s)\n",
		s.GetGitHost(), s.GetGitOwner(), s.GetGitRepo(), s.GetGitBranch())
	fmt.Fprintf(c.stdout, "  Port    : %d · replicas: %d\n", s.GetContainerPort(), s.GetReplicas())
	fmt.Fprintf(c.stdout, "\nTo deploy: kadran deploy %s\n", s.GetAppId())
	return exitOK
}

func (c *cli) runAppList(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app list")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 1 {
		return c.usageError("`app list` takes at most one target, got %d", fs.NArg())
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().ListApps(ctx, &kadranv1.ListAppsRequest{})
	if err != nil {
		return c.fail(fmt.Errorf("could not get apps: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	if len(resp.GetApps()) == 0 {
		fmt.Fprintln(c.stdout, "No apps defined. Start with `kadran app create`.")
		return exitOK
	}

	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSOURCE\tBRANCH\tPORT\tREPLICAS\tRELEASES")
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
	limit := fs.Uint("releases", 10, "number of releases to show")
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	timeout := fs.Duration("timeout", defaultTimeout, "overall time limit")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("usage: kadran app show [options] <name> [target]")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().GetApp(ctx, &kadranv1.GetAppRequest{
		AppId:        fs.Arg(0),
		ReleaseLimit: uint32(*limit), //nolint:gosec // sunucu üst sınırı uyguluyor
	})
	if err != nil {
		return c.fail(fmt.Errorf("could not get the app: %w", err))
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

func (c *cli) printApp(resp *kadranv1.GetAppResponse) {
	s := resp.GetApp().GetSpec()
	fmt.Fprintf(c.stdout, "%s\n", s.GetAppId())
	fmt.Fprintf(c.stdout, "  Source   : %s/%s/%s (%s)\n",
		s.GetGitHost(), s.GetGitOwner(), s.GetGitRepo(), s.GetGitBranch())
	if d := s.GetDomain(); d != "" {
		fmt.Fprintf(c.stdout, "  Domain   : %s\n", d)
	}
	fmt.Fprintf(c.stdout, "  Port     : %d · replicas: %d · health: %s\n",
		s.GetContainerPort(), s.GetReplicas(), s.GetHealthPath())
	if l := s.GetLimits(); l != nil {
		fmt.Fprintf(c.stdout, "  Limits   : %s memory · %d millicpu · blkio %d\n",
			formatSize(l.GetMemoryBytes()), l.GetCpuMillis(), l.GetBlkioWeight())
	}
	// ⚠ Yalnızca ADLAR basılıyor, değerler DEĞİL.
	//
	// `app show` çıktısı ekran görüntüsüne, hata bildirimine ve destek
	// isteğine yapıştırılan şeydir. Kullanıcı kendi sunucusundaki değeri
	// `docker inspect` ile zaten okuyabiliyor; onu istemediği bir yere
	// taşıyan taraf bu araç olmasın.
	//
	// Adların basılması ise şart: "DATABASE_URL ayarlı mı" sorusunun
	// cevabı olmadan bu bölüm hiçbir işe yaramazdı.
	// Hacimler DEGERLERIYLE basiliyor: baglama noktasi sir degil ve
	// "hangi disk nereye bagli" sorusunun cevabi olmadan bu bolum ise
	// yaramaz. Host yolu yine de basilmiyor -- onu executor kuruyor ve
	// burada gostermek istekten geldigi izlenimini verirdi.
	if vols := s.GetVolumes(); len(vols) > 0 {
		fmt.Fprintf(c.stdout, "  Disk     : %d volumes\n", len(vols))
		for _, v := range vols {
			mode := "read-write"
			if v.GetReadOnly() {
				mode = "read-only"
			}
			fmt.Fprintf(c.stdout, "             %s -> %s (%s)\n",
				v.GetName(), v.GetMountPath(), mode)
		}
	}
	if env := s.GetEnv(); len(env) > 0 {
		fmt.Fprintf(c.stdout, "  Env      : %s\n",
			strings.Join(sortedKeys(env), ", "))
		fmt.Fprintf(c.stdout,
			"             (%d variables · values hidden here; `docker inspect` shows them)\n",
			len(env))
	}

	// Hangi sürümün CANLI olduğu ayrı satırda: tablodaki durum derlemenin
	// durumu, trafiğin nereye gittiği değil (K-112). Geri almadan sonra
	// canlı sürüm kesilmiş listenin dışında kalabilir; o zaman bu satır
	// onu gösteren TEK yer.
	//
	// Alanın YOKLUĞU ayrı bir durum: eski bir sunucu bu bilgiyi hiç
	// göndermez (alan eklemek protokol sürümünü artırmıyor). Onu "boş"
	// okumak, trafik akarken "yönlendirilmiyor" demek olurdu.
	releases := resp.GetReleases()
	known := resp.ActiveReleaseId != nil
	active := resp.GetActiveReleaseId()
	switch {
	case !known:
		fmt.Fprintf(c.stdout, "  Live     : unknown — the server does not send this (old version; upgrade the server)\n")
	case active == "":
		fmt.Fprintf(c.stdout, "  Live     : none — no traffic is routed to this app\n")
	case !slices.ContainsFunc(releases, func(r *kadranv1.Release) bool { return r.GetReleaseId() == active }):
		fmt.Fprintf(c.stdout, "  Live     : %s (not in the list below — raise `--releases`)\n", active)
	default:
		fmt.Fprintf(c.stdout, "  Live     : %s\n", active)
	}

	if len(releases) == 0 {
		fmt.Fprintf(c.stdout, "\nNo releases yet. Build one with `kadran deploy %s`.\n", s.GetAppId())
		return
	}

	fmt.Fprintln(c.stdout, "\nReleases:")
	tw := tabwriter.NewWriter(c.stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  RELEASE\tCOMMIT\tBUILD\tTRAFFIC\tIMAGE\tSTARTED")
	for _, r := range releases {
		started := "-"
		if ts := r.GetStartedAt(); ts != nil {
			started = ts.AsTime().Local().Format("2006-01-02 15:04:05")
		}
		traffic := "-"
		switch {
		case !known:
			traffic = "?"
		case r.GetReleaseId() == active:
			traffic = "live"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\t%s\n",
			r.GetReleaseId(), shortSHA(r.GetCommitSha()),
			releaseStatusLabel(r.GetStatus()), traffic, shortImage(r.GetImageId()), started)
	}
	_ = tw.Flush()

	// Başarısız sürümlerin sebebi ayrı basılır: tabloya sığmaz ve asıl
	// aranan bilgi odur.
	for _, r := range releases {
		if r.GetStatus() == kadranv1.ReleaseStatus_RELEASE_STATUS_FAILED && r.GetDetail() != "" {
			fmt.Fprintf(c.stdout, "\n%s failed: %s\n", r.GetReleaseId(), r.GetDetail())
		}
	}
}

func releaseStatusLabel(s kadranv1.ReleaseStatus) string {
	switch s {
	case kadranv1.ReleaseStatus_RELEASE_STATUS_BUILDING:
		return "building"
	case kadranv1.ReleaseStatus_RELEASE_STATUS_BUILT:
		return "built"
	case kadranv1.ReleaseStatus_RELEASE_STATUS_FAILED:
		return "failed"
	default:
		return "unknown"
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
		return "", "", "", fmt.Errorf("-repo is required (e.g. github.com/user/repo)")
	}
	if strings.Contains(s, "://") {
		return "", "", "", fmt.Errorf(
			"-repo is not a URL but a host/owner/name triple (%q) — "+
				"the executor builds the scheme and path", s)
	}
	parts := strings.Split(strings.TrimSuffix(s, ".git"), "/")
	if len(parts) != 3 {
		return "", "", "", fmt.Errorf(
			"-repo must look like host/owner/name (%q)", s)
	}
	return parts[0], parts[1], parts[2], nil
}

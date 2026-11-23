package console

import (
	"context"
	"net"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"

	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
	"github.com/erkanrzgc/kadran/internal/version"
)

// Gerçek zincir: Normalize → Inspect → cli.Run. Sahte Run yok: çalıştırıcının
// kurduğu argümanlar ve Env gerçek CLI'a gider. Yalnız iki kanca sahte:
// Dial (bufconn üstündeki sahte kadrand; istenen hedef kaydedilir) ve Command
// (ssh başlatılmaz; çağrı kaydedilir). Kayıtlı sunucular geçici yapılandırma
// dizininde: kutu ve CLI aynı servers.json'u okur (R12).

const (
	chainCommit = "0123456789abcdef0123456789abcdef01234567"
	chainApp    = "web"
)

// chainServer, zincir testinin sahte kadrand'ı: CLI'ın sunucuda yaptığı
// işleri kaydeder.
type chainServer struct {
	kadranv1.UnimplementedKadranServiceServer
	mu       sync.Mutex
	listApps int
	deploys  []*kadranv1.DeployRequest
}

// seen, ListApps sayısı ve sunucuya gelen deploy istekleri.
func (s *chainServer) seen() (int, []*kadranv1.DeployRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.listApps, slices.Clone(s.deploys)
}

func (s *chainServer) Ping(context.Context, *kadranv1.PingRequest) (*kadranv1.PingResponse, error) {
	return &kadranv1.PingResponse{ProtocolVersion: version.Protocol, TotpRequired: proto.Bool(false)}, nil
}

func (s *chainServer) ListApps(context.Context, *kadranv1.ListAppsRequest) (*kadranv1.ListAppsResponse, error) {
	s.mu.Lock()
	s.listApps++
	s.mu.Unlock()
	return &kadranv1.ListAppsResponse{Apps: []*kadranv1.App{{Spec: &kadranv1.AppSpec{
		AppId: chainApp, GitHost: "github.com", GitOwner: "demo", GitRepo: chainApp, GitBranch: "main", ContainerPort: 8080, Replicas: 1,
	}}}}, nil
}

func (s *chainServer) Deploy(req *kadranv1.DeployRequest, stream grpc.ServerStreamingServer[kadranv1.DeployResponse]) error {
	s.mu.Lock()
	s.deploys = append(s.deploys, &kadranv1.DeployRequest{AppId: req.GetAppId(), CommitSha: req.GetCommitSha()})
	s.mu.Unlock()
	if err := stream.Send(&kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Accepted{Accepted: &kadranv1.DeployAccepted{
		ReleaseId: "r1", AppId: req.GetAppId(), CommitSha: req.GetCommitSha(),
	}}}); err != nil {
		return err
	}
	return stream.Send(&kadranv1.DeployResponse{Event: &kadranv1.DeployResponse_Succeeded{Succeeded: &kadranv1.DeploySucceeded{
		ReleaseId: "r1", ImageId: "sha256:chain",
	}}})
}

// chain, gerçek CLI'ın sunucuya ve ssh'a yaptığı her çağrının kaydı.
type chain struct {
	srv *chainServer

	mu       sync.Mutex
	runs     [][]string
	dials    []string
	commands [][]string
}

func (c *chain) snapshot() (runs [][]string, dials []string, commands [][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return slices.Clone(c.runs), slices.Clone(c.dials), slices.Clone(c.commands)
}

func (c *chain) reset() {
	c.mu.Lock()
	c.runs, c.dials, c.commands = nil, nil, nil
	c.mu.Unlock()
	c.srv.mu.Lock()
	c.srv.listApps, c.srv.deploys = 0, nil
	c.srv.mu.Unlock()
}

// run, gerçek cli.Run: yalnız Dial ve Command kancaları kaydedici sahtelerle
// sarılır; argümanlar ve geri kalan Env çalıştırıcının kurduğu gibi.
func (c *chain) run(ctx context.Context, args []string, env cli.Env) int {
	c.mu.Lock()
	c.runs = append(c.runs, slices.Clone(args))
	c.mu.Unlock()
	dial := env.Dial
	env.Dial = func(ctx context.Context, raw string) (*client.Client, *kadranv1.PingResponse, error) {
		c.mu.Lock()
		c.dials = append(c.dials, raw)
		c.mu.Unlock()
		return dial(ctx, raw)
	}
	env.Command = func(ctx context.Context, name string, a ...string) *exec.Cmd {
		c.mu.Lock()
		c.commands = append(c.commands, append([]string{name}, a...))
		c.mu.Unlock()
		// Hiçbir şey başlatılmaz: var olmayan bir program.
		return exec.CommandContext(ctx, "kadran-console-chain-test-no-such-program")
	}
	return cli.Run(ctx, args, env)
}

// chainServers, geçici yapılandırma dizinine yazılan kayıt.
func chainServers() []servers.Server {
	return []servers.Server{
		{Name: "hetzner", Target: "kadran-client@h.example", Admin: "root@h.example"},
		{Name: "gcp", Target: "kadran-client@g.example", Admin: "ops@g.example", Sudo: true},
	}
}

func newChain(t *testing.T) (*harness, *chain) {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("APPDATA", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("HOME", cfg)
	if _, err := (servers.Store{}).Update(func([]servers.Server) ([]servers.Server, error) { return chainServers(), nil }); err != nil {
		t.Fatal(err)
	}

	lis := bufconn.Listen(1 << 20)
	grpcSrv := grpc.NewServer()
	srv := &chainServer{}
	kadranv1.RegisterKadranServiceServer(grpcSrv, srv)
	go func() { _ = grpcSrv.Serve(lis) }()
	t.Cleanup(grpcSrv.Stop)
	conns := NewConns(func(tg client.Target) (*client.Client, error) {
		return client.DialWithDialer(tg, func(context.Context, string) (net.Conn, error) { return lis.Dial() })
	})

	c := &chain{srv: srv}
	h := newHarness(t, func(d *RunnerDeps) {
		d.Run = c.run
		d.Conns = conns
		d.Servers = servers.Store{}.Load
	})
	return h, c
}

func TestRealChainTypedServerFlag(t *testing.T) {
	h, c := newChain(t)
	id, exit := h.exec("hetzner", "app list -s hetzner")
	if exit != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", exit, h.output(id, StreamErr))
	}
	runs, dials, commands := c.snapshot()
	if len(runs) != 1 || !slices.Equal(runs[0], []string{"app", "list", "-s", "hetzner"}) {
		t.Fatalf("cli.Run argümanları %q", runs)
	}
	// CLI'ın gerçekten bağlandığı yer seçili sunucunun kayıttaki hedefi.
	if !slices.Equal(dials, []string{"kadran-client@h.example"}) {
		t.Fatalf("bağlanılan hedefler %q", dials)
	}
	if lists, _ := c.srv.seen(); lists != 1 || len(commands) != 0 || h.boxCount() != 0 {
		t.Fatalf("ListApps %d, ssh %q, kutu %d", lists, commands, h.boxCount())
	}
	if !strings.Contains(h.output(id, StreamOut), "github.com/demo/web") {
		t.Fatalf("çıktı %q", h.output(id, ""))
	}

	// Seçili sunucudan farklı bir -s CLI'a hiç ulaşmaz.
	c.reset()
	if _, exit := h.exec("hetzner", "app list -s gcp"); exit == cli.ExitOK {
		t.Fatal("başka sunucunun -s'i çalıştı")
	}
	if runs, dials, _ := c.snapshot(); len(runs) != 0 || len(dials) != 0 {
		t.Fatalf("başka sunucu: Run %q, bağlantı %q", runs, dials)
	}
}

func TestRealChainHelpFlagActsOnNothing(t *testing.T) {
	h, c := newChain(t)
	id, _ := h.exec("hetzner", "deploy -h")
	runs, dials, commands := c.snapshot()
	if len(runs) != 1 || !slices.Equal(runs[0], []string{"deploy", "-h", "-s", "hetzner"}) {
		t.Fatalf("cli.Run argümanları %q", runs)
	}
	if _, deploys := c.srv.seen(); len(dials) != 0 || len(commands) != 0 || len(deploys) != 0 || h.boxCount() != 0 {
		t.Fatalf("yardım bir şey yaptı: bağlantı %q, ssh %q, deploy %d, kutu %d", dials, commands, len(deploys), h.boxCount())
	}
	if !strings.Contains(h.output(id, ""), "-commit") {
		t.Fatalf("yardım metni yok: %q", h.output(id, ""))
	}
}

func TestRealChainApprovedChangeMatchesBox(t *testing.T) {
	h, c := newChain(t)
	line := "deploy -commit " + chainCommit + " " + chainApp

	// Reddedilen kutu: CLI hiç çalışmaz, sunucuya gidilmez.
	h.setAnswer(false)
	if _, exit := h.exec("hetzner", line); exit == cli.ExitOK {
		t.Fatal("reddedilen kutu çalıştı")
	}
	if runs, dials, _ := c.snapshot(); len(runs) != 0 || len(dials) != 0 {
		t.Fatalf("reddedilen kutu: Run %q, bağlantı %q", runs, dials)
	}
	if _, deploys := c.srv.seen(); len(deploys) != 0 {
		t.Fatalf("reddedilen kutu: deploy %d", len(deploys))
	}

	h.setAnswer(true)
	id, exit := h.exec("hetzner", line)
	if exit != cli.ExitOK {
		t.Fatalf("exit %d stderr %q", exit, h.output(id, StreamErr))
	}
	box := h.lastBox()
	// 40 haneli commit boşluksuz koşudur: kutu onu 32 birimlik parçalara
	// böler; girinti atılıp birleşince satır bire bir aynı olmalı.
	joined := strings.ReplaceAll(box.Detail, "\n    ", "")
	if !strings.Contains(joined, "Server: hetzner") || !strings.Contains(joined, "Command: "+line) {
		t.Fatalf("kutu:\n%s", box.Detail)
	}
	runs, dials, commands := c.snapshot()
	if len(runs) != 1 || !slices.Equal(runs[0], []string{"deploy", "-commit", chainCommit, chainApp, "-s", "hetzner"}) {
		t.Fatalf("cli.Run argümanları %q", runs)
	}
	// Kutudaki sunucu ve satır, CLI'ın bağlandığı hedef ve sunucuya giden istekle aynı.
	if !slices.Equal(dials, []string{"kadran-client@h.example"}) || len(commands) != 0 {
		t.Fatalf("bağlantı %q, ssh %q", dials, commands)
	}
	if _, deploys := c.srv.seen(); len(deploys) != 1 || deploys[0].GetAppId() != chainApp || deploys[0].GetCommitSha() != chainCommit {
		t.Fatalf("sunucuya giden deploy %v", deploys)
	}
}

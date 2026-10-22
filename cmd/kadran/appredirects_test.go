package main

import (
	"bytes"
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/domaincheck/dnstest"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// fakeRedirectRPC, yalnız app RPC'lerini cevaplayan sahte sunucudur.
type fakeRedirectRPC struct {
	kadranv1.KadranServiceClient
	old       bool // eski sunucu: cevapta Www yok
	updateErr error
	created   *kadranv1.AppSpec
	updated   *kadranv1.UpdateAppRequest
	show      *kadranv1.AppSpec
}

func (f *fakeRedirectRPC) reply(s *kadranv1.AppSpec) *kadranv1.AppSpec {
	out, ok := proto.Clone(s).(*kadranv1.AppSpec)
	if !ok {
		panic("proto.Clone AppSpec döndürmedi")
	}
	if f.old {
		out.Www = nil
		out.Redirects = nil
	} else if out.Www == nil {
		out.Www = proto.Bool(false)
	}
	return out
}

func (f *fakeRedirectRPC) CreateApp(_ context.Context, in *kadranv1.CreateAppRequest, _ ...grpc.CallOption) (*kadranv1.CreateAppResponse, error) {
	f.created = in.GetSpec()
	return &kadranv1.CreateAppResponse{App: &kadranv1.App{Spec: f.reply(in.GetSpec())}}, nil
}

func (f *fakeRedirectRPC) UpdateApp(_ context.Context, in *kadranv1.UpdateAppRequest, _ ...grpc.CallOption) (*kadranv1.UpdateAppResponse, error) {
	f.updated = in
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	spec := &kadranv1.AppSpec{AppId: in.GetAppId(), Domain: in.GetDomain(), Www: proto.Bool(in.GetWww()), Redirects: in.GetRedirectAdd()}
	return &kadranv1.UpdateAppResponse{App: &kadranv1.App{Spec: f.reply(spec)}, RedirectDetail: "redirect detail"}, nil
}

func (f *fakeRedirectRPC) GetApp(context.Context, *kadranv1.GetAppRequest, ...grpc.CallOption) (*kadranv1.GetAppResponse, error) {
	return &kadranv1.GetAppResponse{App: &kadranv1.App{Spec: f.show}}, nil
}

func redirectCLI(res cliResolver, rpc *fakeRedirectRPC) (*cli, *bytes.Buffer, *bytes.Buffer) {
	c, out, errOut := newTestCLI("")
	c.resolver = res
	c.sshHostname = func(_ context.Context, host string) (string, error) { return host, nil }
	c.dial = func(context.Context, string) (*client.Client, *kadranv1.PingResponse, error) {
		return client.NewWithRPC(rpc), nil, nil
	}
	return c, out, errOut
}

var rightDNS = cliResolver{
	"ip4 example.com":     {"203.0.113.10"},
	"ip4 www.example.com": {"203.0.113.10"},
	"ip4 old.com":         {"203.0.113.10"},
}

func TestAppCreateSendsWWWByDefault(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(rightDNS, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "example.com", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if rpc.created.Www == nil || !rpc.created.GetWww() {
		t.Errorf("Www = %v, true bekleniyordu", rpc.created.Www)
	}
}

func TestAppCreateNoWWW(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(rightDNS, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "example.com", "-no-www", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if rpc.created.Www == nil || rpc.created.GetWww() {
		t.Errorf("Www = %v, false bekleniyordu", rpc.created.Www)
	}
}

func TestAppCreateRepeatsRedirect(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(rightDNS, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "example.com",
		"-redirect", "old.com", "-redirect", "other.com", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if got := strings.Join(rpc.created.GetRedirects(), ","); got != "old.com,other.com" {
		t.Errorf("Redirects = %q", got)
	}
}

func TestAppUpdateWWWAndNoWWWConflict(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(rightDNS, rpc)
	code := c.runAppUpdate(t.Context(), []string{"-www", "-no-www", "web"})
	if code != exitUsage {
		t.Fatalf("çıkış %d, beklenen %d", code, exitUsage)
	}
	if !strings.Contains(errOut.String(), "-www and -no-www cannot be used together") {
		t.Errorf("mesaj yok: %q", errOut)
	}
	if rpc.updated != nil {
		t.Error("çakışmada sunucuya gidildi")
	}
}

func TestAppUpdateRedirectRm(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(rightDNS, rpc)
	code := c.runAppUpdate(t.Context(), []string{"-redirect-rm", "old.com", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if got := strings.Join(rpc.updated.GetRedirectRemove(), ","); got != "old.com" {
		t.Errorf("RedirectRemove = %q", got)
	}
	if rpc.updated.Www != nil {
		t.Error("verilmeyen -www isteğe girdi")
	}
}

func TestAppShowPrintsRedirects(t *testing.T) {
	rpc := &fakeRedirectRPC{show: &kadranv1.AppSpec{
		AppId: "web", Domain: "example.com", Www: proto.Bool(true), Redirects: []string{"old.com"}}}
	c, out, errOut := redirectCLI(nil, rpc)
	if code := c.runAppShow(t.Context(), []string{"web", "kimse@203.0.113.10"}); code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if !strings.Contains(out.String(), "Redirects: www.example.com, old.com → example.com (308)") {
		t.Errorf("Redirects satırı yok:\n%s", out)
	}
}

func TestAppCreateWarnsOnOldServer(t *testing.T) {
	rpc := &fakeRedirectRPC{old: true}
	c, out, errOut := redirectCLI(rightDNS, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "example.com", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if !strings.Contains(errOut.String(), "kadran: warning: this server does not support redirects (www, -redirect); upgrade kadrand") {
		t.Errorf("uyarı yok:\n%s", errOut)
	}
	if strings.Contains(out.String(), "Redirects:") {
		t.Errorf("eski sunucuda Redirects satırı basıldı:\n%s", out)
	}
}

// oldServerEmptyUpdate, v0.7.x sunucusunun ad alanlarını tanımadığı için boş
// saydığı güncellemeye verdiği cevap (internal/api/appupdate.go ile aynı metin).
var oldServerEmptyUpdate = status.Error(codes.InvalidArgument,
	"no field given — this call would only advance the timestamp, that is, a silent no-op")

// Yalnız ad bayrağı taşıyan güncelleme eski sunucuda "boş" sayılır: kullanıcı
// anlamsız bir "no field given" değil, yükseltme ipucu görmeli.
func TestAppUpdateNamesOnlyOnOldServerHintsUpgrade(t *testing.T) {
	for _, args := range [][]string{
		{"-no-www", "web", "kimse@203.0.113.10"},
		{"-www", "web", "kimse@203.0.113.10"},
		{"-redirect", "old.com", "web", "kimse@203.0.113.10"},
		{"-redirect-rm", "old.com", "web", "kimse@203.0.113.10"},
	} {
		rpc := &fakeRedirectRPC{old: true, updateErr: oldServerEmptyUpdate}
		c, _, errOut := redirectCLI(rightDNS, rpc)
		if code := c.runAppUpdate(t.Context(), args); code != exitError {
			t.Errorf("%v: çıkış %d, %d bekleniyordu", args, code, exitError)
		}
		if !strings.Contains(errOut.String(), "this server does not support redirects (www, -redirect); upgrade kadrand") {
			t.Errorf("%v: yükseltme ipucu yok:\n%s", args, errOut)
		}
		if strings.Contains(errOut.String(), "no field given") {
			t.Errorf("%v: eski sunucunun anlamsız hatası basıldı:\n%s", args, errOut)
		}
	}
}

// Yeni sunucunun doğrulama hatası (ör. dokuzuncu ad) olduğu gibi basılmalı;
// ad bayrağı başka bir alanla birlikteyse "no field given" da eski sunucu
// demek değildir.
func TestAppUpdateNamesValidationErrorIsShownAsIs(t *testing.T) {
	cases := []struct {
		err  error
		args []string
		want string
	}{
		{status.Error(codes.InvalidArgument, "too many redirect names (9, limit 8)"),
			[]string{"-redirect", "old.com", "web", "kimse@203.0.113.10"}, "too many redirect names (9, limit 8)"},
		{oldServerEmptyUpdate,
			[]string{"-www", "-branch", "main", "web", "kimse@203.0.113.10"}, "no field given"},
	}
	for _, tc := range cases {
		rpc := &fakeRedirectRPC{updateErr: tc.err}
		c, _, errOut := redirectCLI(rightDNS, rpc)
		if code := c.runAppUpdate(t.Context(), tc.args); code != exitError {
			t.Errorf("%v: çıkış %d, %d bekleniyordu", tc.args, code, exitError)
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%v: gerçek hata basılmadı:\n%s", tc.args, errOut)
		}
		if strings.Contains(errOut.String(), "upgrade kadrand") {
			t.Errorf("%v: doğrulama hatası yükseltme ipucuna çevrildi:\n%s", tc.args, errOut)
		}
	}
}

var wrongWWW = cliResolver{
	"ip4 example.com":     {"203.0.113.10"},
	"ip4 www.example.com": {"198.51.100.7"},
}

func TestAppCreateRedirectDNSWarnsButSucceeds(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(wrongWWW, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "example.com", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	for _, want := range []string{"www.example.com does not point at the server", "A     www"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr %q içermiyor:\n%s", want, errOut)
		}
	}
}

func TestAppCreateSkipDNSCheckSkipsRedirects(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, _, errOut := redirectCLI(wrongWWW, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "example.com", "-skip-dns-check", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if strings.Contains(errOut.String(), "does not point at the server") {
		t.Errorf("-skip-dns-check yönlendirme uyarısını susturmadı:\n%s", errOut)
	}
}

// Yönlendirme adının uyarısı karara uymalı (22 Eki, canlıda görüldü): Stop'ta
// ad sunucuyu göstermiyor; Warn'da (DNS firması sunucuyu cevaplıyor, yalnız
// bu makinenin önbelleği eski) "göstermiyor" demek yanlış, `domain check`'in
// gerekçesi ad başta basılmalı. İkisinde de çıkış kodu değişmez.
func TestAppUpdateRedirectDNSWarningMatchesVerdict(t *testing.T) {
	cases := []struct {
		name      string
		local     cliResolver
		vercel    bool
		want, bad []string
	}{
		{name: "stop", local: wrongWWW,
			want: []string{"www.example.com does not point at the server yet", "A     www"},
			bad:  []string{"already answers", "from its cache"}},
		{name: "warn", vercel: true, local: cliResolver{
			"ip4 ns1.vercel-dns.com": {"192.0.2.1"},
			"ip4 example.com":        {"203.0.113.10"},
			"ip4 www.example.com":    {"198.51.100.7"},
		},
			want: []string{
				"www.example.com: your DNS host (Vercel) already answers 203.0.113.10; this machine still sees 198.51.100.7 from its cache",
				"Let's Encrypt usually sees the new record",
				"www.example.com is redirected and gets its own certificate once it resolves to the server",
			},
			bad: []string{"does not point at the server", "A     www"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _, errOut := redirectCLI(tc.local, &fakeRedirectRPC{})
			if tc.vercel {
				c.nsResolver = cliNS{"example.com": {"ns1.vercel-dns.com."}}
				c.exchange = dnstest.Exchange(map[string]dnstest.Answer{"192.0.2.1": {AA: true, Addrs: []string{"203.0.113.10"}, TTL: 600}})
			}
			code := c.runAppUpdate(t.Context(), []string{"-domain", "example.com", "-www", "web", "kimse@203.0.113.10"})
			if code != exitOK {
				t.Fatalf("çıkış %d, %d bekleniyordu\n%s", code, exitOK, errOut)
			}
			for _, want := range tc.want {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("stderr %q içermiyor:\n%s", want, errOut)
				}
			}
			for _, bad := range tc.bad {
				if strings.Contains(errOut.String(), bad) {
					t.Errorf("stderr %q içermemeli:\n%s", bad, errOut)
				}
			}
		})
	}
}

func TestAppCreateSubdomainHasNoWWW(t *testing.T) {
	rpc := &fakeRedirectRPC{}
	c, out, errOut := redirectCLI(cliResolver{"ip4 app.example.com": {"203.0.113.10"}}, rpc)
	code := c.runAppCreate(t.Context(), []string{"-repo", "github.com/u/r", "-domain", "app.example.com", "web", "kimse@203.0.113.10"})
	if code != exitOK {
		t.Fatalf("çıkış %d\n%s", code, errOut)
	}
	if strings.Contains(out.String(), "Redirects:") {
		t.Errorf("alt adda Redirects satırı:\n%s", out)
	}
	if strings.Contains(errOut.String(), "www.") {
		t.Errorf("alt ad için www denetimi yapıldı:\n%s", errOut)
	}
}

// barrierResolver, ikinci eşzamanlı çağrı gelene kadar bekler.
type barrierResolver struct {
	wg   *sync.WaitGroup
	seen *sync.Map
}

func (b barrierResolver) LookupIP(_ context.Context, network, host string) ([]net.IP, error) {
	// Her ad için yalnız İLK ip4 sorgusu bariyere girer.
	if _, dup := b.seen.LoadOrStore(host, true); network == "ip4" && !dup {
		b.wg.Done()
		b.wg.Wait()
	}
	return []net.IP{net.ParseIP("203.0.113.10")}, nil
}

func TestRedirectChecksRunInParallel(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(2)
	c, _, _ := newTestCLI("")
	c.resolver = barrierResolver{wg: &wg, seen: &sync.Map{}}
	c.sshHostname = func(_ context.Context, host string) (string, error) { return host, nil }

	done := make(chan struct{})
	go func() {
		c.checkRedirectNames(t.Context(), []string{"a.example.com", "b.example.com"}, "kimse@203.0.113.10", false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("denetimler paralel koşmadı: ikinci çağrı gelmedi")
	}
}

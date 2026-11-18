package github

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// DefaultClientID ve DefaultAppSlug "Kadran Deploy" GitHub App kimliğidir (gizli değil).
	DefaultClientID = "Iv23lizBm3zlCdYH3NQ5"
	DefaultAppSlug  = "kadran-deploy"
)

var (
	appSlugPattern  = regexp.MustCompile(`^[a-z0-9-]{1,100}$`)
	clientIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

	errBadConfig = errors.New("github: invalid configuration")
	errClosed    = errors.New("github: service closed")
)

// Config Service ayarlarıdır. ep ve sleep yalnız testler içindir (dışa açık değil).
type Config struct {
	ClientID, AppSlug, StateFile string
	Now                          func() time.Time
	HTTP                         *http.Client
	ep                           endpoints
	sleep                        func(ctx context.Context, d time.Duration) error
}

// flowStatus son cihaz akışının sonucudur (fmu ile korunur). Cihaz kodu burada YOKTUR.
type flowStatus struct {
	pending *kadranv1.GitHubUserCodePrompt
	failed  bool
	failure kadranv1.GitHubFailure
}

// Service kadran-git'in GitHub yüzüdür. Jeton, cihaz kodu ve yanıt gövdeleri
// bu paketin dışına çıkmaz.
//
// Kilit sırası: lifeMu -> (fmu | covMu | tk.mu); yoklama goroutine'i lifeMu almaz.
type Service struct {
	clientID, appSlug string
	now               func() time.Time
	sleep             func(context.Context, time.Duration) error
	c                 *client
	tk                *tokens

	baseCtx    context.Context
	baseCancel context.CancelFunc

	lifeMu sync.Mutex // Start/Disconnect/Close'u sıralar; flow ve closed'u korur
	flow   *flowRun
	closed bool

	fmu sync.Mutex
	fs  flowStatus

	// Kapsam önbelleği ve arka plan yenilemesi (hepsi covMu ile korunur).
	covMu    sync.Mutex
	cov      coverage
	gen      uint64      // Disconnect ve yeni bağlantıyla artar; bayat yenileme sonucu atılır
	refresh  *refreshRun // sürmekte olan tek uçuşlu yenileme
	bgClosed bool        // Close sonrası yeni yenileme başlamaz
	bg       sync.WaitGroup
}

// New Service kurar. Yalnız bozuk ayar hata döner; durum dosyası kullanılamazsa
// Service yine kurulur (Status FAILED/INTERNAL, Decide NotConnected).
func New(cfg Config) (*Service, error) {
	if cfg.ClientID == "" {
		cfg.ClientID = DefaultClientID
	}
	if cfg.AppSlug == "" {
		cfg.AppSlug = DefaultAppSlug
	}
	if cfg.StateFile == "" || !filepath.IsAbs(cfg.StateFile) || !clientIDPattern.MatchString(cfg.ClientID) || !appSlugPattern.MatchString(cfg.AppSlug) {
		return nil, errBadConfig
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.sleep == nil {
		cfg.sleep = timerSleep
	}
	if cfg.ep == (endpoints{}) {
		cfg.ep = defaultEndpoints()
	}
	c := newClient(cfg.ep, cfg.HTTP)
	c.now = cfg.Now
	s := &Service{clientID: cfg.ClientID, appSlug: cfg.AppSlug, now: cfg.Now, sleep: cfg.sleep, c: c}
	s.baseCtx, s.baseCancel = context.WithCancel(context.Background())
	tk, err := newTokens(cfg.StateFile, cfg.ClientID, c, cfg.Now)
	if err != nil {
		slog.Error("github state directory unusable, starting disconnected")
		tk = &tokens{path: cfg.StateFile, clientID: cfg.ClientID, c: c, now: cfg.Now}
		s.fs = flowStatus{failed: true, failure: kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL}
	}
	s.tk = tk
	if tk.Identity() != nil { // yeniden başlatmadan sonra önbelleği ısıt
		s.covMu.Lock()
		s.startRefreshLocked()
		s.covMu.Unlock()
	}
	return s, nil
}

// timerSleep ctx bitince erken döner.
func timerSleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (s *Service) installURL() string {
	return "https://github.com/apps/" + s.appSlug + "/installations/new"
}

// Status bağlantı durumunu döner; jeton içermez. Kapsam önbelleği bir dakikadan
// eskiyse yenilenir. reconnect_required atomik okunur: yenileme sürerken beklemez.
func (s *Service) Status(_ context.Context) (*kadranv1.GitHubConnection, error) {
	conn := &kadranv1.GitHubConnection{
		State:             kadranv1.GitHubConnection_STATE_NOT_CONNECTED,
		InstallUrl:        s.installURL(),
		ReconnectRequired: s.tk.ReconnectRequired(),
	}
	if id := s.tk.Identity(); id != nil {
		conn.State = kadranv1.GitHubConnection_STATE_CONNECTED
		if ownerPattern.MatchString(id.login) { // durum dosyasından gelen ad yeniden doğrulanır
			conn.Login = id.login
		}
		conn.ConnectedAt = timestamppb.New(id.connectedAt)
		conn.Installations = s.cachedInstallations()
	}
	s.fmu.Lock()
	fs := s.fs
	s.fmu.Unlock()
	switch {
	case fs.pending != nil:
		conn.State = kadranv1.GitHubConnection_STATE_PENDING
		conn.Pending = cloneMsg(fs.pending)
	case fs.failed:
		conn.State = kadranv1.GitHubConnection_STATE_FAILED
		conn.Failure = fs.failure
	}
	return conn, nil
}

// Disconnect akışı iptal eder (bitmesini bekler), durum dosyasını siler,
// önbelleği ve bellekteki jetonları temizler. Tekrar çağrı zararsızdır.
func (s *Service) Disconnect(ctx context.Context) error {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	s.stopFlowLocked()
	err := s.tk.reset()
	s.invalidateCoverage(false) // sürmekte olan yenilemeyi iptal eder, bekler, sonucunu atar
	s.setStatus(flowStatus{})
	return err
}

// Close yoklama goroutine'ini iptal eder ve bitmesini bekler; tekrar çağrılabilir.
func (s *Service) Close() {
	s.baseCancel() // sürmekte olan Start'ı da keser
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	s.closed = true
	s.stopFlowLocked()
	s.covMu.Lock()
	s.bgClosed = true
	s.covMu.Unlock()
	s.bg.Wait() // baseCtx iptal edildi: yenilemeler kısa sürede çıkar
}

// withToken call'u geçerli jetonla çağırır. 401 gelirse jeton bir kez zorla yenilenir
// ve bir kez yeniden denenir; ikinci 401 bağlantıyı düşürür (ErrReconnect).
func (s *Service) withToken(ctx context.Context, call func(token string) error) error {
	tok, err := s.tk.Token(ctx)
	if err != nil {
		return err
	}
	err = call(tok)
	if !errors.Is(err, ErrUnauthorized) {
		return err
	}
	tok2, err := s.tk.ForceRefresh(ctx, tok)
	if err != nil {
		return err
	}
	err = call(tok2)
	if errors.Is(err, ErrUnauthorized) {
		return s.tk.Rejected(tok2)
	}
	return err
}

func (s *Service) getJSONAuth(ctx context.Context, path string, out any) error {
	return s.withToken(ctx, func(token string) error { return s.c.getJSON(ctx, path, token, out) })
}

// Tarball depo commit'inin arşivini açar. Karar zaten DecisionArchive olmalıdır;
// buradaki her hata derlemeyi düşürür, anonim yola düşülmez.
func (s *Service) Tarball(ctx context.Context, owner, repo, sha string) (io.ReadCloser, error) {
	if err := validRepoRef(owner, repo); err != nil {
		return nil, err
	}
	if !validSHA(sha) {
		return nil, errBadSHA
	}
	var rc io.ReadCloser
	err := s.withToken(ctx, func(token string) error {
		r, err := s.c.tarball(ctx, owner, repo, sha, token)
		if err == nil {
			rc = r
		}
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrForbidden) {
			s.markStaleIfListed(owner, repo) // kurulum listesi eskimiş olabilir
		}
		return nil, err
	}
	return rc, nil
}

// Archive önce Decide, karar DecisionArchive ise Tarball çağırır. Tarball hatasında
// (DecisionArchive, nil, err) döner: karar verilmiştir, geri düşülmez.
func (s *Service) Archive(ctx context.Context, owner, repo, sha string) (Decision, io.ReadCloser, error) {
	d, err := s.Decide(ctx, owner, repo)
	if err != nil || d != DecisionArchive {
		return d, nil, err
	}
	rc, err := s.Tarball(ctx, owner, repo, sha)
	if err != nil {
		return DecisionArchive, nil, err
	}
	return DecisionArchive, rc, nil
}

// cloneMsg, proto mesajının derin kopyasını aynı tipte döner (proto.Clone
// tipi korur; iddia hiç düşmez, yine de denetlenir).
func cloneMsg[M proto.Message](m M) M {
	c, _ := proto.Clone(m).(M)
	return c
}

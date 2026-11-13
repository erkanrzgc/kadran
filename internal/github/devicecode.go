package github

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"regexp"
	"time"

	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	deviceCodePath  = "/login/device/code"
	deviceGrantType = "urn:ietf:params:oauth:grant-type:device_code"

	defaultPollInterval = 5 * time.Second
	slowDownStep        = 5 * time.Second
	maxPollInterval     = 5 * time.Minute
	maxCodeLifetime     = time.Hour
)

var (
	userCodePattern = regexp.MustCompile(`^[A-Za-z0-9-]{4,32}$`)

	errDeviceRejected = errors.New("github: device flow request rejected")
)

// flowRun tek yoklama goroutine'inin tutamağıdır. done'u yalnız goroutine kapatır.
type flowRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type codeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int64  `json:"expires_in"`
	Interval        int64  `json:"interval"`
	Error           string `json:"error"`
}

type pollResponse struct {
	refreshResponse
	Interval int64 `json:"interval"`
}

type pollKind int

const (
	pollWait pollKind = iota
	pollSlow
	pollFail
	pollToken
	pollStop
)

type pollResult struct {
	kind     pollKind
	failure  kadranv1.GitHubFailure
	interval time.Duration // pollSlow: yeni aralık
	tok      refreshResponse
}

// StartDeviceFlow cihaz akışını başlatır. Eski akış iptal edilir ve bitmesi beklenir;
// mevcut bağlantı yeni akış başarılı olana kadar korunur. Cihaz kodu yalnız yoklama
// goroutine'inin belleğinde kalır.
func (s *Service) StartDeviceFlow(ctx context.Context) (*kadranv1.GitHubUserCodePrompt, error) {
	s.lifeMu.Lock()
	defer s.lifeMu.Unlock()
	if s.closed {
		return nil, errClosed
	}
	s.stopFlowLocked()

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer context.AfterFunc(s.baseCtx, cancel)()
	dc, failure, err := s.requestCode(reqCtx)
	if err != nil {
		s.setStatus(flowStatus{failed: true, failure: failure})
		return nil, err
	}
	now := s.now()
	lifetime := min(time.Duration(dc.ExpiresIn)*time.Second, maxCodeLifetime)
	expiresAt := now.Add(lifetime)
	prompt := &kadranv1.GitHubUserCodePrompt{
		UserCode: dc.UserCode, VerificationUri: dc.VerificationURI,
		InstallUrl: s.installURL(), ExpiresAt: timestamppb.New(expiresAt),
	}
	interval := defaultPollInterval
	if dc.Interval > 0 {
		interval = min(time.Duration(dc.Interval)*time.Second, maxPollInterval)
	}
	runCtx, runCancel := context.WithCancel(s.baseCtx)
	fr := &flowRun{cancel: runCancel, done: make(chan struct{})}
	s.flow = fr
	s.setStatus(flowStatus{pending: prompt})
	go s.poll(runCtx, fr, dc.DeviceCode, interval, expiresAt)
	return proto.Clone(prompt).(*kadranv1.GitHubUserCodePrompt), nil
}

// stopFlowLocked akışı iptal eder ve goroutine'in çıkmasını bekler (lifeMu tutulurken).
func (s *Service) stopFlowLocked() {
	if fr := s.flow; fr != nil {
		fr.cancel()
		<-fr.done
		s.flow = nil
	}
}

func (s *Service) setStatus(fs flowStatus) {
	s.fmu.Lock()
	s.fs = fs
	s.fmu.Unlock()
}

// requestCode cihaz kodunu ister. Yalnız client_id gider: kapsam da sır da yok.
func (s *Service) requestCode(ctx context.Context) (codeResponse, kadranv1.GitHubFailure, error) {
	var dc codeResponse
	if err := s.c.postForm(ctx, deviceCodePath, url.Values{"client_id": {s.clientID}}, &dc); err != nil {
		return dc, failureFor(err), err
	}
	if dc.Error != "" {
		return dc, failureForCode(dc.Error), errDeviceRejected
	}
	if !validCodeResponse(dc) {
		return dc, kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL, errResponse
	}
	return dc, 0, nil
}

func validCodeResponse(dc codeResponse) bool {
	u, err := url.Parse(dc.VerificationURI)
	if dc.DeviceCode == "" || len(dc.DeviceCode) > 256 || dc.ExpiresIn <= 0 || !userCodePattern.MatchString(dc.UserCode) {
		return false
	}
	// Kullanıcıya gösterilecek adres github.com'a sabitlenir: sahte bir adres kimlik avına yol açardı.
	return err == nil && len(dc.VerificationURI) <= 200 && u.Scheme == "https" && u.Host == "github.com" &&
		u.User == nil && u.RawQuery == "" && u.Fragment == "" && !u.ForceQuery
}

// failureFor taşıma ve durum hatalarını GitHubFailure'a çevirir.
func failureFor(err error) kadranv1.GitHubFailure {
	switch {
	case errors.Is(err, ErrRateLimited):
		return kadranv1.GitHubFailure_GIT_HUB_FAILURE_RATE_LIMITED
	case errors.Is(err, errTransport), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return kadranv1.GitHubFailure_GIT_HUB_FAILURE_NETWORK
	}
	return kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL
}

// failureForCode OAuth hata kodunu GitHubFailure'a çevirir.
func failureForCode(code string) kadranv1.GitHubFailure {
	switch code {
	case "expired_token":
		return kadranv1.GitHubFailure_GIT_HUB_FAILURE_CODE_EXPIRED
	case "access_denied":
		return kadranv1.GitHubFailure_GIT_HUB_FAILURE_ACCESS_DENIED
	case "device_flow_disabled":
		return kadranv1.GitHubFailure_GIT_HUB_FAILURE_DEVICE_FLOW_DISABLED
	case "incorrect_client_credentials", "unsupported_grant_type":
		return kadranv1.GitHubFailure_GIT_HUB_FAILURE_BAD_CLIENT
	}
	return kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL
}

// poll Service ömrüne bağlı TEK yoklama döngüsüdür. Önce bekler, sonra sorar.
func (s *Service) poll(ctx context.Context, fr *flowRun, deviceCode string, interval time.Duration, expiresAt time.Time) {
	defer close(fr.done)
	for {
		if s.sleep(ctx, interval) != nil {
			return
		}
		if !s.now().Before(expiresAt) {
			s.finishFailed(ctx, kadranv1.GitHubFailure_GIT_HUB_FAILURE_CODE_EXPIRED)
			return
		}
		res := s.pollOnce(ctx, deviceCode, interval)
		switch res.kind {
		case pollSlow:
			interval = res.interval
		case pollFail:
			s.finishFailed(ctx, res.failure)
			return
		case pollToken:
			s.complete(ctx, res.tok)
			return
		case pollStop:
			return
		}
	}
}

// pollOnce tek yoklama isteği yapar. Geçici hatalar (taşıma, 5xx) pollWait olur;
// akış yerel süre dolana kadar sürer.
func (s *Service) pollOnce(ctx context.Context, deviceCode string, interval time.Duration) pollResult {
	form := url.Values{
		"client_id":   {s.clientID},
		"device_code": {deviceCode},
		"grant_type":  {deviceGrantType},
	}
	var out pollResponse
	if err := s.c.postForm(ctx, refreshPath, form, &out); err != nil {
		return pollError(ctx, err)
	}
	switch out.Error {
	case "":
		if out.AccessToken == "" {
			return pollResult{kind: pollFail, failure: kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL}
		}
		return pollResult{kind: pollToken, tok: out.refreshResponse}
	case "authorization_pending":
		return pollResult{kind: pollWait}
	case "slow_down":
		next := interval + slowDownStep
		if srv := time.Duration(out.Interval) * time.Second; srv > next {
			next = srv
		}
		return pollResult{kind: pollSlow, interval: min(next, maxPollInterval)}
	}
	return pollResult{kind: pollFail, failure: failureForCode(out.Error)}
}

func pollError(ctx context.Context, err error) pollResult {
	switch {
	case ctx.Err() != nil:
		return pollResult{kind: pollStop}
	case errors.Is(err, ErrRateLimited):
		return pollResult{kind: pollFail, failure: kadranv1.GitHubFailure_GIT_HUB_FAILURE_RATE_LIMITED}
	case errors.Is(err, ErrUnauthorized), errors.Is(err, ErrForbidden), errors.Is(err, ErrNotFound),
		errors.Is(err, ErrMoved), errors.Is(err, errResponse), errors.Is(err, errTooLarge), errors.Is(err, errBadPath):
		return pollResult{kind: pollFail, failure: kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL}
	}
	return pollResult{kind: pollWait}
}

// finishFailed akışı FAILED bırakır; iptal edilmiş akış sonuç yazmaz.
func (s *Service) finishFailed(ctx context.Context, f kadranv1.GitHubFailure) {
	if ctx.Err() != nil {
		return
	}
	s.setStatus(flowStatus{failed: true, failure: f})
}

type userResponse struct {
	Login string `json:"login"`
	ID    int64  `json:"id"`
}

// complete kullanıcıyı okur, bağlantıyı kalıcılaştırır ve kapsamı çeker.
// Kapsam hatası bağlantıyı bozmaz.
func (s *Service) complete(ctx context.Context, tok refreshResponse) {
	var u userResponse
	if err := s.c.getJSON(ctx, "/user", tok.AccessToken, &u); err != nil {
		s.finishFailed(ctx, failureFor(err))
		return
	}
	if ctx.Err() != nil {
		return
	}
	if !ownerPattern.MatchString(u.Login) {
		s.finishFailed(ctx, kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL)
		return
	}
	now := s.now()
	err := s.tk.install(state{
		Version: stateVersion, ClientID: s.clientID, Login: u.Login, UserID: u.ID,
		AccessToken: tok.AccessToken, AccessExpiresAt: expiry(now, tok.ExpiresIn),
		RefreshToken: tok.RefreshToken, RefreshExpiresAt: expiry(now, tok.RefreshExpiresIn),
		ConnectedAt: now,
	})
	if err != nil {
		slog.Error("github connection could not be persisted")
		s.finishFailed(ctx, kadranv1.GitHubFailure_GIT_HUB_FAILURE_INTERNAL)
		return
	}
	s.invalidateCoverage(true) // yeni kuşak + tek uçuşlu arka plan ısıtması
	s.setStatus(flowStatus{})
}

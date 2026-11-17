package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"github.com/erkanrzgc/kadran/internal/github"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const goodSHA = "0123456789abcdef0123456789abcdef01234567"

// fakeSvc gitService arayüzünün sahte uygulamasıdır; çağrıları sayar.
type fakeSvc struct {
	decision    github.Decision
	decideErr   error
	tarball     io.ReadCloser
	tarballErr  error
	decideCalls atomic.Int32
	statusCalls atomic.Int32
	tarCalls    int
	branchSHA   string
	branchErr   error
	branchCalls int
	prompt      *kadranv1.GitHubUserCodePrompt
	startErr    error
	statusConn  *kadranv1.GitHubConnection
	discErr     error
}

func (f *fakeSvc) StartDeviceFlow(context.Context) (*kadranv1.GitHubUserCodePrompt, error) {
	return f.prompt, f.startErr
}
func (f *fakeSvc) Status(context.Context) (*kadranv1.GitHubConnection, error) {
	f.statusCalls.Add(1)
	return f.statusConn, nil
}
func (f *fakeSvc) Disconnect(context.Context) error { return f.discErr }
func (f *fakeSvc) ResolveBranch(context.Context, string, string, string) (string, error) {
	f.branchCalls++
	return f.branchSHA, f.branchErr
}
func (f *fakeSvc) Decide(context.Context, string, string) (github.Decision, error) {
	f.decideCalls.Add(1)
	return f.decision, f.decideErr
}
func (f *fakeSvc) Tarball(context.Context, string, string, string) (io.ReadCloser, error) {
	f.tarCalls++
	return f.tarball, f.tarballErr
}

// closeTracker gövdenin kapatıldığını kaydeder.
type closeTracker struct {
	io.Reader
	closed bool
}

func (c *closeTracker) Close() error { c.closed = true; return nil }

// errReader data'yı verdikten sonra hata döner.
type errReader struct {
	data []byte
	err  error
}

func (e *errReader) Read(p []byte) (int, error) {
	if len(e.data) == 0 {
		return 0, e.err
	}
	n := copy(p, e.data)
	e.data = e.data[n:]
	return n, nil
}

// fakeStream grpc.ServerStreamingServer arayüzünü karşılar ve iletileri toplar.
type fakeStream struct {
	grpc.ServerStream
	ctx    context.Context
	sent   []*kadranv1.FetchArchiveResponse
	calls  int
	failOn int // >0: bu siradaki Send hata doner
}

func (s *fakeStream) Context() context.Context { return s.ctx }
func (s *fakeStream) Send(m *kadranv1.FetchArchiveResponse) error {
	s.calls++
	if s.failOn > 0 && s.calls == s.failOn {
		return errors.New("send failed https://x?sig=SECRET")
	}
	// Gercek Send serilestirir; arabellek yeniden kullanildigi icin kopya tut.
	s.sent = append(s.sent, proto.Clone(m).(*kadranv1.FetchArchiveResponse))
	return nil
}

func fetch(t *testing.T, f *fakeSvc, req *kadranv1.FetchArchiveRequest) (*fakeStream, error) {
	t.Helper()
	st := &fakeStream{ctx: context.Background()}
	err := (&fetchServer{svc: f}).FetchArchive(req, st)
	return st, err
}

func goodReq() *kadranv1.FetchArchiveRequest {
	return &kadranv1.FetchArchiveRequest{Owner: "octo", Repo: "app", CommitSha: goodSHA}
}

func TestFetchSendsDecisionFirst(t *testing.T) {
	body := &closeTracker{Reader: strings.NewReader("tar")}
	f := &fakeSvc{decision: github.DecisionArchive, tarball: body}

	st, err := fetch(t, f, goodReq())

	if err != nil {
		t.Fatalf("FetchArchive: %v", err)
	}
	if len(st.sent) < 2 {
		t.Fatalf("sent %d messages, want decision + chunk", len(st.sent))
	}
	if st.sent[0].GetDecision() != kadranv1.FetchArchiveResponse_DECISION_ARCHIVE {
		t.Fatalf("first message = %v, want ARCHIVE decision", st.sent[0])
	}
	for i, m := range st.sent[1:] {
		if m.GetChunk() == nil {
			t.Fatalf("message %d is not a chunk: %v", i+1, m)
		}
	}
	if !body.closed {
		t.Error("body not closed")
	}
}

func TestFetchStreamsArchiveInChunks(t *testing.T) {
	payload := make([]byte, 3*chunkSize+123)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	f := &fakeSvc{decision: github.DecisionArchive, tarball: &closeTracker{Reader: bytes.NewReader(payload)}}

	st, err := fetch(t, f, goodReq())

	if err != nil {
		t.Fatalf("FetchArchive: %v", err)
	}
	var got []byte
	for _, m := range st.sent[1:] {
		c := m.GetChunk()
		if len(c) > chunkSize {
			t.Fatalf("chunk of %d bytes exceeds %d", len(c), chunkSize)
		}
		got = append(got, c...)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("reassembled %d bytes, want %d", len(got), len(payload))
	}
}

func TestFetchPreDecisionErrorSendsNoMessage(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code codes.Code
	}{
		"other":    {errors.New("secret ghu_token detail"), codes.Unavailable},
		"deadline": {context.DeadlineExceeded, codes.DeadlineExceeded},
		"canceled": {context.Canceled, codes.Canceled},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeSvc{decideErr: tc.err}

			st, err := fetch(t, f, goodReq())

			if len(st.sent) != 0 {
				t.Fatalf("sent %d messages before the decision", len(st.sent))
			}
			if status.Code(err) != tc.code {
				t.Fatalf("code = %v, want %v", status.Code(err), tc.code)
			}
			if strings.Contains(err.Error(), "ghu_") {
				t.Fatalf("status leaks error text: %v", err)
			}
			if f.tarCalls != 0 {
				t.Error("tarball requested without a decision")
			}
		})
	}
}

func TestFetchRejectsInvalidRefsWithoutDecision(t *testing.T) {
	for name, req := range map[string]*kadranv1.FetchArchiveRequest{
		"bad owner": {Owner: "../x", Repo: "app", CommitSha: goodSHA},
		"bad repo":  {Owner: "octo", Repo: "..", CommitSha: goodSHA},
		"short sha": {Owner: "octo", Repo: "app", CommitSha: "abc"},
		"upper sha": {Owner: "octo", Repo: "app", CommitSha: strings.ToUpper(goodSHA)},
		"empty":     {},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeSvc{decision: github.DecisionArchive}

			st, err := fetch(t, f, req)

			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
			}
			if len(st.sent) != 0 || f.decideCalls.Load() != 0 || f.tarCalls != 0 {
				t.Fatalf("sent=%d decide=%d tarball=%d, want all zero", len(st.sent), f.decideCalls.Load(), f.tarCalls)
			}
		})
	}
}

func TestFetchNotCoveredIsOnlyMessage(t *testing.T) {
	for d, want := range map[github.Decision]kadranv1.FetchArchiveResponse_Decision{
		github.DecisionNotConnected: kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED,
		github.DecisionNotCovered:   kadranv1.FetchArchiveResponse_DECISION_NOT_COVERED,
	} {
		t.Run(want.String(), func(t *testing.T) {
			f := &fakeSvc{decision: d}

			st, err := fetch(t, f, goodReq())

			if err != nil {
				t.Fatalf("FetchArchive: %v", err)
			}
			if len(st.sent) != 1 || st.sent[0].GetDecision() != want {
				t.Fatalf("sent = %v, want exactly [%v]", st.sent, want)
			}
			if f.tarCalls != 0 {
				t.Error("tarball requested for a non-archive decision")
			}
		})
	}
}

func TestFetchArchiveErrorAfterDecisionIsStatus(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		code codes.Code
	}{
		"not found":    {github.ErrNotFound, codes.NotFound},
		"forbidden":    {github.ErrForbidden, codes.PermissionDenied},
		"reconnect":    {github.ErrReconnect, codes.FailedPrecondition},
		"not conn":     {github.ErrNotConnected, codes.FailedPrecondition},
		"rate limited": {github.ErrRateLimited, codes.ResourceExhausted},
		"moved":        {github.ErrMoved, codes.NotFound},
		"wrapped":      {fmt.Errorf("x: %w", github.ErrNotFound), codes.NotFound},
		"unknown":      {errors.New("https://signed.example/?token=SECRET"), codes.Unknown},
	} {
		t.Run(name, func(t *testing.T) {
			f := &fakeSvc{decision: github.DecisionArchive, tarballErr: tc.err}

			st, err := fetch(t, f, goodReq())

			if status.Code(err) != tc.code {
				t.Fatalf("code = %v, want %v", status.Code(err), tc.code)
			}
			if len(st.sent) != 1 || st.sent[0].GetDecision() != kadranv1.FetchArchiveResponse_DECISION_ARCHIVE {
				t.Fatalf("sent = %v, want exactly the ARCHIVE decision", st.sent)
			}
			if strings.Contains(err.Error(), "SECRET") || strings.Contains(err.Error(), "signed.example") {
				t.Fatalf("status leaks error text: %v", err)
			}
		})
	}
	t.Run("moved text", func(t *testing.T) {
		_, err := fetch(t, &fakeSvc{decision: github.DecisionArchive, tarballErr: github.ErrMoved}, goodReq())
		if !strings.Contains(err.Error(), "repository moved on GitHub") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestFetchMidStreamReadErrorIsStatusAndClosesBody(t *testing.T) {
	body := &closeTracker{Reader: &errReader{data: []byte("part"), err: errors.New("conn reset https://x?sig=SECRET")}}
	f := &fakeSvc{decision: github.DecisionArchive, tarball: body}

	_, err := fetch(t, f, goodReq())

	if status.Code(err) != codes.Unknown || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v, want clean Unknown", err)
	}
	if !body.closed {
		t.Error("body not closed after read error")
	}
}

func TestAdminResolveBranchValidatesInputs(t *testing.T) {
	f := &fakeSvc{branchSHA: goodSHA}
	a := &adminServer{svc: f}
	ctx := context.Background()

	for name, req := range map[string]*kadranv1.ResolveBranchRequest{
		"bad owner":  {Owner: "a/b", Repo: "r", Branch: "main"},
		"bad branch": {Owner: "o", Repo: "r", Branch: "-x"},
		"empty":      {},
	} {
		if _, err := a.ResolveBranch(ctx, req); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s: code = %v, want InvalidArgument", name, status.Code(err))
		}
	}
	if f.branchCalls != 0 {
		t.Fatal("service called for invalid input")
	}
	resp, err := a.ResolveBranch(ctx, &kadranv1.ResolveBranchRequest{Owner: "o", Repo: "r", Branch: "feature/x"})
	if err != nil || resp.GetCommitSha() != goodSHA {
		t.Fatalf("resp = %v, err = %v", resp, err)
	}
}

func TestAdminMapsErrorsWithoutLeaking(t *testing.T) {
	leak := errors.New("device_code=SECRET")
	a := &adminServer{svc: &fakeSvc{startErr: leak, branchErr: github.ErrNotFound, discErr: leak}}
	ctx := context.Background()

	_, err := a.StartDeviceFlow(ctx, &kadranv1.StartDeviceFlowRequest{})
	if status.Code(err) != codes.Unknown || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("StartDeviceFlow err = %v", err)
	}
	_, err = a.DisconnectGit(ctx, &kadranv1.DisconnectGitRequest{})
	if status.Code(err) != codes.Unknown || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("DisconnectGit err = %v", err)
	}
	_, err = a.ResolveBranch(ctx, &kadranv1.ResolveBranchRequest{Owner: "o", Repo: "r", Branch: "main"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("ResolveBranch err = %v, want NotFound", err)
	}
}

func TestAdminPassesThroughPromptAndStatus(t *testing.T) {
	prompt := &kadranv1.GitHubUserCodePrompt{UserCode: "ABCD-1234", VerificationUri: "https://github.com/login/device"}
	conn := &kadranv1.GitHubConnection{State: kadranv1.GitHubConnection_STATE_CONNECTED, Login: "octo"}
	a := &adminServer{svc: &fakeSvc{prompt: prompt, statusConn: conn}}
	ctx := context.Background()

	p, err := a.StartDeviceFlow(ctx, &kadranv1.StartDeviceFlowRequest{})
	if err != nil || p.GetPrompt().GetUserCode() != "ABCD-1234" {
		t.Fatalf("prompt = %v, err = %v", p, err)
	}
	s, err := a.GetGitStatus(ctx, &kadranv1.GetGitStatusRequest{})
	if err != nil || s.GetConnection().GetLogin() != "octo" {
		t.Fatalf("status = %v, err = %v", s, err)
	}
	if _, err := a.DisconnectGit(ctx, &kadranv1.DisconnectGitRequest{}); err != nil {
		t.Fatalf("DisconnectGit: %v", err)
	}
}

func TestFetchSendFailureOnFirstChunkIsStatusAndClosesBody(t *testing.T) {
	body := &closeTracker{Reader: strings.NewReader("tar")}
	f := &fakeSvc{decision: github.DecisionArchive, tarball: body}
	st := &fakeStream{ctx: context.Background(), failOn: 2}

	err := (&fetchServer{svc: f}).FetchArchive(goodReq(), st)

	if status.Code(err) != codes.Unknown || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v, want clean Unknown", err)
	}
	if !body.closed {
		t.Error("body not closed after send failure")
	}
	if len(st.sent) != 1 {
		t.Fatalf("sent %d messages, want only the decision", len(st.sent))
	}
}

func TestFetchUnknownDecisionIsPreDecisionError(t *testing.T) {
	f := &fakeSvc{decision: github.Decision(99)}

	st, err := fetch(t, f, goodReq())

	if status.Code(err) != codes.Unavailable {
		t.Fatalf("code = %v, want Unavailable", status.Code(err))
	}
	if len(st.sent) != 0 || f.tarCalls != 0 {
		t.Fatalf("sent=%d tarball=%d, want zero", len(st.sent), f.tarCalls)
	}
}

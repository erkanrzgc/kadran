//go:build linux

// Bu testler gerçek unix soketine BAĞLANIR; Windows'ta AF_UNIX connect bu
// ortamda çalışmıyor, bu yüzden yalnız Linux.

package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/erkanrzgc/kadran/internal/audit"
	"github.com/erkanrzgc/kadran/internal/dockerdrv"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

const (
	testDecisionTimeout = 200 * time.Millisecond
)

// socketBase, testler TMP değişkenini değiştirmeden ÖNCEKİ kısa temp dizinidir.
var socketBase = os.TempDir()

type fetchHandler func(*kadranv1.FetchArchiveRequest, kadranv1.GitFetchService_FetchArchiveServer) error

type fakeGit struct {
	kadranv1.UnimplementedGitFetchServiceServer
	handle fetchHandler
}

func (f *fakeGit) FetchArchive(req *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
	return f.handle(req, s)
}

// shortSocket, Windows'ta da sığan kısa bir unix soket yolu verir
// (t.TempDir yolu test adını içerir ve sun_path sınırını aşabilir).
func shortSocket(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp(socketBase, "kx")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

func startFakeGit(t *testing.T, h fetchHandler) string {
	t.Helper()
	sock := shortSocket(t, "g.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	kadranv1.RegisterGitFetchServiceServer(gs, &fakeGit{handle: h})
	go func() { _ = gs.Serve(l) }()
	t.Cleanup(gs.Stop)
	return sock
}

// startSilentListener bağlantıyı kabul eder ama hiç yazmaz.
func startSilentListener(t *testing.T) string {
	t.Helper()
	sock := shortSocket(t, "s.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		_ = l.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range conns {
			_ = c.Close()
		}
	})
	return sock
}

// okFetcher başarı yolu testleri içindir: yavaş bir CI el sıkışması sonucu
// NOT_CONNECTED'a çevirmesin diye cömert süre.
func okFetcher(sock string) GitFetcher {
	return &gitFetcher{socket: sock, decisionTimeout: 5 * time.Second, chunkIdle: time.Minute}
}

// shortFetcher yalnız zaman aşımı testlerinde kullanılır.
func shortFetcher(sock string) *gitFetcher {
	return &gitFetcher{socket: sock, decisionTimeout: testDecisionTimeout, chunkIdle: time.Minute}
}

func decide(d kadranv1.FetchArchiveResponse_Decision) *kadranv1.FetchArchiveResponse {
	return &kadranv1.FetchArchiveResponse{Kind: &kadranv1.FetchArchiveResponse_Decision_{Decision: d}}
}

func chunk(b []byte) *kadranv1.FetchArchiveResponse {
	return &kadranv1.FetchArchiveResponse{Kind: &kadranv1.FetchArchiveResponse_Chunk{Chunk: b}}
}

// sendArchive, ARCHIVE kararını ve ardından gövdeyi 7 baytlık parçalarla yollar.
func sendArchive(s kadranv1.GitFetchService_FetchArchiveServer, body []byte) error {
	if err := s.Send(decide(dArchive)); err != nil {
		return err
	}
	for len(body) > 0 {
		n := min(7, len(body))
		if err := s.Send(chunk(body[:n])); err != nil {
			return err
		}
		body = body[n:]
	}
	return nil
}

func archiveHandler(body []byte) fetchHandler {
	return func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		return sendArchive(s, body)
	}
}

func decisionHandler(d kadranv1.FetchArchiveResponse_Decision) fetchHandler {
	return func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		return s.Send(decide(d))
	}
}

func statusHandler(c codes.Code) fetchHandler {
	return func(*kadranv1.FetchArchiveRequest, kadranv1.GitFetchService_FetchArchiveServer) error {
		return status.Error(c, "secret server text ghu_leak")
	}
}

// dockerMock, Docker soketinde derleme isteklerini sayan sahte motordur.
type dockerMock struct {
	mu      sync.Mutex
	builds  int
	remote  bool
	tarType bool
	body    []byte
}

func (m *dockerMock) snapshot() (int, bool, bool, []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.builds, m.remote, m.tarType, m.body
}

func startDockerMock(t *testing.T, srv *Server) *dockerMock {
	t.Helper()
	m := &dockerMock{}
	sock := shortSocket(t, "d.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"ApiVersion":"1.48","MinAPIVersion":"1.44"}`))
			return
		}
		data, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.builds++
		m.remote = r.URL.Query().Has("remote")
		m.tarType = r.Header.Get("Content-Type") == "application/x-tar"
		m.body = data
		m.mu.Unlock()
		_, _ = w.Write([]byte(`{"aux":{"ID":"sha256:fixture"}}`))
	}))
	hs.Listener = l
	hs.Start()
	t.Cleanup(hs.Close)
	srv.docker = dockerdrv.New(sock, "")
	return m
}

func archiveWithContent(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	body := "FROM scratch\n"
	for _, h := range []*tar.Header{
		{Name: "octocat-Hello-World-7fd1a60/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "octocat-Hello-World-7fd1a60/Dockerfile", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body))},
		{Name: "octocat-Hello-World-7fd1a60/src/app.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 5},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		switch h.Size {
		case int64(len(body)):
			_, _ = tw.Write([]byte(body))
		case 5:
			_, _ = tw.Write([]byte("hello"))
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func tempContexts(t *testing.T) string {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	t.Setenv("TMP", dir)
	t.Setenv("TEMP", dir)
	return dir
}

func noLeftoverContext(t *testing.T, dir string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "kadran-context-*.tar"))
	if len(files) != 0 {
		t.Fatalf("build context left on disk: %v", files)
	}
}

func TestBuildUsesPublicPathWhenNotConnected(t *testing.T) {
	for name, sock := range map[string]func(*testing.T) string{
		"no socket":     func(t *testing.T) string { return shortSocket(t, "yok.sock") },
		"NOT_CONNECTED": func(t *testing.T) string { return startFakeGit(t, decisionHandler(dNotConnected)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t)
			m := startDockerMock(t, srv)
			srv.gitFetch = okFetcher(sock(t))
			if err := srv.ImageBuild(validBuildRequest(), newBuildStream()); err != nil {
				t.Fatal(err)
			}
			builds, remote, tarType, _ := m.snapshot()
			if builds != 1 || !remote || tarType {
				t.Fatalf("public path not used: builds=%d remote=%v tar=%v", builds, remote, tarType)
			}
			if got := contextSource(t, srv); got != "public" {
				t.Fatalf("context_source=%q", got)
			}
		})
	}
}

func TestBuildUsesPublicPathWhenNotCovered(t *testing.T) {
	srv := newTestServer(t)
	m := startDockerMock(t, srv)
	srv.gitFetch = okFetcher(startFakeGit(t, decisionHandler(dNotCovered)))
	if err := srv.ImageBuild(validBuildRequest(), newBuildStream()); err != nil {
		t.Fatal(err)
	}
	builds, remote, tarType, _ := m.snapshot()
	if builds != 1 || !remote || tarType || contextSource(t, srv) != "public" {
		t.Fatalf("public path not used: builds=%d remote=%v tar=%v", builds, remote, tarType)
	}
}

func TestPublicBuildSurvivesUnresponsiveGitService(t *testing.T) {
	for name, sock := range map[string]func(*testing.T) string{
		"silent listener":       startSilentListener,
		"status before message": func(t *testing.T) string { return startFakeGit(t, statusHandler(codes.Internal)) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := newTestServer(t)
			m := startDockerMock(t, srv)
			srv.gitFetch = shortFetcher(sock(t))
			start := time.Now()
			if err := srv.ImageBuild(validBuildRequest(), newBuildStream()); err != nil {
				t.Fatal(err)
			}
			if time.Since(start) > testDecisionTimeout+time.Second {
				t.Fatalf("public build waited %v", time.Since(start))
			}
			if builds, remote, _, _ := m.snapshot(); builds != 1 || !remote {
				t.Fatalf("public path not reached: builds=%d remote=%v", builds, remote)
			}
		})
	}
}

func TestBuildUsesTarWhenPrivate(t *testing.T) {
	dir := tempContexts(t)
	srv := newTestServer(t)
	m := startDockerMock(t, srv)
	srv.gitFetch = okFetcher(startFakeGit(t, archiveHandler(archiveWithContent(t))))
	stream := newBuildStream()
	if err := srv.ImageBuild(validBuildRequest(), stream); err != nil {
		t.Fatal(err)
	}
	builds, remote, tarType, body := m.snapshot()
	if builds != 1 || remote || !tarType {
		t.Fatalf("tar path not used: builds=%d remote=%v tar=%v", builds, remote, tarType)
	}
	names := map[string]string{}
	tr := tar.NewReader(bytes.NewReader(body))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(tr)
		names[h.Name] = string(data)
	}
	if names["Dockerfile"] != "FROM scratch\n" || names["src/app.txt"] != "hello" || len(names) != 2 {
		t.Fatalf("rewritten entries wrong: %v", names)
	}
	if n := len(stream.sent); n < 1 || stream.sent[n-1].GetImageId() != "sha256:fixture" {
		t.Fatal("private build did not produce positive image evidence")
	}
	rec := lastRecord(t, srv)
	if paramOf(t, rec.ParamsJSON, "context_source") != "github-app" ||
		paramOf(t, rec.ParamsJSON, "source") != "github.com/octocat/Hello-World" {
		t.Fatalf("audit params wrong: %s", rec.ParamsJSON)
	}
	noLeftoverContext(t, dir)
}

func TestPrivateFetchErrorFailsBuild(t *testing.T) {
	good := archiveWithContent(t)
	cases := map[string]fetchHandler{
		"status mid-stream": func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
			_ = s.Send(decide(dArchive))
			_ = s.Send(chunk(good[:10]))
			return status.Error(codes.NotFound, "ghu_leak")
		},
		"status right after decision": func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
			_ = s.Send(decide(dArchive))
			return status.Error(codes.PermissionDenied, "ghu_leak")
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			dir := tempContexts(t)
			srv := newTestServer(t)
			m := startDockerMock(t, srv)
			srv.gitFetch = okFetcher(startFakeGit(t, h))
			err := srv.ImageBuild(validBuildRequest(), newBuildStream())
			if err == nil || strings.Contains(err.Error(), "ghu_leak") || strings.Contains(err.Error(), "secret") {
				t.Fatalf("expected a sanitized failure, got %v", err)
			}
			if builds, _, _, _ := m.snapshot(); builds != 0 {
				t.Fatal("docker was called after a private fetch failure")
			}
			noLeftoverContext(t, dir)
			rec := lastRecord(t, srv)
			if rec.Outcome != audit.OutcomeFailure || paramOf(t, rec.ParamsJSON, "context_source") != "github-app" {
				t.Fatalf("failure not audited with the github-app source: %+v", rec)
			}
		})
	}
}

func TestFetchStatusTextsAreFixed(t *testing.T) {
	want := map[codes.Code]string{
		codes.NotFound:           "private repository not found or app not installed on it",
		codes.PermissionDenied:   "app lacks permission for this repository",
		codes.FailedPrecondition: "GitHub connection needs attention: run kadran github status",
		codes.ResourceExhausted:  "GitHub rate limit reached, retry later",
		codes.Internal:           "private archive download failed",
	}
	for code, text := range want {
		h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
			_ = s.Send(decide(dArchive))
			return status.Error(code, "ghu_leak")
		}
		d, f, err := okFetcher(startFakeGit(t, h)).Fetch(context.Background(), validGitSource())
		if d != dArchive || f != nil || err == nil || err.Error() != text {
			t.Fatalf("%v: decision=%v file=%v err=%v", code, d, f, err)
		}
	}
}

func TestFetchTruncatedArchiveFailsBuild(t *testing.T) {
	good := archiveWithContent(t)
	dir := tempContexts(t)
	d, f, err := okFetcher(startFakeGit(t, archiveHandler(good[:len(good)-9]))).Fetch(context.Background(), validGitSource())
	if d != dArchive || f != nil || err == nil {
		t.Fatalf("truncated archive accepted: %v %v %v", d, f, err)
	}
	noLeftoverContext(t, dir)
}

func TestFetchSecondDecisionFailsBuild(t *testing.T) {
	good := archiveWithContent(t)
	h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		_ = s.Send(decide(dArchive))
		_ = s.Send(chunk(good[:8]))
		return s.Send(decide(dNotConnected))
	}
	dir := tempContexts(t)
	d, f, err := okFetcher(startFakeGit(t, h)).Fetch(context.Background(), validGitSource())
	if d != dArchive || f != nil || err == nil {
		t.Fatalf("second decision accepted: %v %v %v", d, f, err)
	}
	noLeftoverContext(t, dir)
}

func TestFetchMessageWithoutChunkFailsBuild(t *testing.T) {
	h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		_ = s.Send(decide(dArchive))
		return s.Send(&kadranv1.FetchArchiveResponse{})
	}
	dir := tempContexts(t)
	d, f, err := okFetcher(startFakeGit(t, h)).Fetch(context.Background(), validGitSource())
	if d != dArchive || f != nil || err == nil {
		t.Fatalf("empty message accepted: %v %v %v", d, f, err)
	}
	noLeftoverContext(t, dir)
}

func TestFetchChunkFirstIsPreDecision(t *testing.T) {
	for name, first := range map[string]*kadranv1.FetchArchiveResponse{
		"chunk":       chunk([]byte("x")),
		"empty":       {},
		"unspecified": decide(kadranv1.FetchArchiveResponse_DECISION_UNSPECIFIED),
		"unknown":     decide(kadranv1.FetchArchiveResponse_Decision(99)),
	} {
		t.Run(name, func(t *testing.T) {
			h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
				return s.Send(first)
			}
			d, f, err := okFetcher(startFakeGit(t, h)).Fetch(context.Background(), validGitSource())
			if d != dNotConnected || f != nil || err != nil {
				t.Fatalf("pre-decision protocol error not NOT_CONNECTED: %v %v %v", d, f, err)
			}
		})
	}
}

func TestFetchNotCoveredDoesNotReadOn(t *testing.T) {
	canceled := make(chan struct{})
	h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		_ = s.Send(decide(dNotCovered))
		select {
		case <-s.Context().Done():
			close(canceled)
		case <-time.After(5 * time.Second):
		}
		return nil
	}
	d, f, err := okFetcher(startFakeGit(t, h)).Fetch(context.Background(), validGitSource())
	if d != dNotCovered || f != nil || err != nil {
		t.Fatalf("got %v %v %v", d, f, err)
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("stream was not canceled after NOT_COVERED")
	}
}

func TestArchiveChecksStillApply(t *testing.T) {
	for name, body := range map[string][]byte{
		"symlink": gitArchive(t, &tar.Header{Name: "r/", Typeflag: tar.TypeDir}, &tar.Header{Name: "r/l", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}),
		"dotdot":  gitArchive(t, &tar.Header{Name: "r/../outside", Typeflag: tar.TypeReg}),
		"duplicate": gitArchive(t, &tar.Header{Name: "r/f", Typeflag: tar.TypeReg},
			&tar.Header{Name: "r/f", Typeflag: tar.TypeReg}),
	} {
		t.Run(name, func(t *testing.T) {
			dir := tempContexts(t)
			d, f, err := okFetcher(startFakeGit(t, archiveHandler(body))).Fetch(context.Background(), validGitSource())
			if d != dArchive || f != nil || err == nil {
				t.Fatalf("bad archive accepted: %v %v %v", d, f, err)
			}
			noLeftoverContext(t, dir)
		})
	}
}

func TestFetchHonorsBuildContextCancel(t *testing.T) {
	h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		_ = s.Send(decide(dArchive))
		<-s.Context().Done()
		return nil
	}
	dir := tempContexts(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(3*testDecisionTimeout, cancel)
	d, f, err := okFetcher(startFakeGit(t, h)).Fetch(ctx, validGitSource())
	if d != dArchive || f != nil || err == nil {
		t.Fatalf("got %v %v %v", d, f, err)
	}
	noLeftoverContext(t, dir)
}

// TestArchiveDownloadMayOutlastDecisionWindow, 5 sn'lik pencerenin ARCHIVE
// kararıyla BİTTİĞİNİ kanıtlar: karar penceresi 200 ms, indirme 600 ms+ sürer.
// Zamanlayıcı durdurulmazsa (mutant: `!timer.Stop()` yerine yalnız bayrak
// bakmak) akış 200. ms'de iptal edilir ve bu test düşer.
func TestArchiveDownloadMayOutlastDecisionWindow(t *testing.T) {
	good := archiveWithContent(t)
	h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		if err := s.Send(decide(dArchive)); err != nil {
			return err
		}
		time.Sleep(3 * testDecisionTimeout)
		for len(good) > 0 {
			n := min(16, len(good))
			if err := s.Send(chunk(good[:n])); err != nil {
				return err
			}
			good = good[n:]
		}
		return nil
	}
	dir := tempContexts(t)
	d, f, err := shortFetcher(startFakeGit(t, h)).Fetch(context.Background(), validGitSource())
	if d != dArchive || f == nil || err != nil {
		t.Fatalf("slow archive failed: %v %v %v", d, f, err)
	}
	defer closePrivateContext(f)
	tr := tar.NewReader(f)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != "Dockerfile" {
		t.Fatalf("rewritten content wrong: %v %v", hdr, err)
	}
	if body, _ := io.ReadAll(tr); string(body) != "FROM scratch\n" {
		t.Fatalf("body wrong: %q", body)
	}
	_ = dir
}

func TestArchiveStallFailsBuild(t *testing.T) {
	good := archiveWithContent(t)
	h := func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
		_ = s.Send(decide(dArchive))
		_ = s.Send(chunk(good[:10]))
		<-s.Context().Done() // sessizlik
		return nil
	}
	dir := tempContexts(t)
	g := okFetcher(startFakeGit(t, h)).(*gitFetcher)
	g.chunkIdle = testDecisionTimeout
	start := time.Now()
	d, f, err := g.Fetch(context.Background(), validGitSource())
	if d != dArchive || f != nil || err == nil || err.Error() != "private archive download stalled" {
		t.Fatalf("stall not reported: %v %v %v", d, f, err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("stall took %v", time.Since(start))
	}
	noLeftoverContext(t, dir)
}

func TestPreDecisionLogCategories(t *testing.T) {
	cases := map[string]struct {
		sock   func(*testing.T) string
		reason string
	}{
		"timeout": {startSilentListener, "reason=timeout"},
		"status":  {func(t *testing.T) string { return startFakeGit(t, statusHandler(codes.Internal)) }, "reason=status"},
		"protocol": {func(t *testing.T) string {
			return startFakeGit(t, func(_ *kadranv1.FetchArchiveRequest, s kadranv1.GitFetchService_FetchArchiveServer) error {
				return s.Send(chunk([]byte("x")))
			})
		}, "reason=protocol"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			g := shortFetcher(tc.sock(t))
			g.log = slog.New(slog.NewTextHandler(&buf, nil))
			if d, f, err := g.Fetch(context.Background(), validGitSource()); d != dNotConnected || f != nil || err != nil {
				t.Fatalf("got %v %v %v", d, f, err)
			}
			if !strings.Contains(buf.String(), tc.reason) || strings.Contains(buf.String(), "secret") || strings.Contains(buf.String(), "ghu_") {
				t.Fatalf("log category wrong or leaking: %s", buf.String())
			}
		})
	}
}

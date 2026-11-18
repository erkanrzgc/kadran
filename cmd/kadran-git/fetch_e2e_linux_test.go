//go:build linux

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/erkanrzgc/kadran/internal/exec"
	"github.com/erkanrzgc/kadran/internal/github"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/peercred"
)

// Gerçek fetchServer (gerçek sunucu seçenekleri, gerçek peercred el sıkışması)
// ile gerçek yürütücü istemcisi (exec.NewGitFetcher) aynı unix soketinde
// konuşur: iki tarafın protokol sözleşmesi (git.proto durum makinesi) birlikte
// sınanır (son inceleme M6). Yalnız arayan politikası test sürecinin uid'ine
// açılır; üretim politikası (yalnız root) değişmez ve aşağıda kontrol grubu
// olarak ölçülür.

const fetchDockerfile = "FROM scratch\n"

// codeloadTarGz, GitHub'ın biçimindeki küçük bir arşivdir: tek kök dizin ve altında Dockerfile.
func codeloadTarGz(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, h := range []*tar.Header{
		{Name: "octo-app-0123456/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: "octo-app-0123456/Dockerfile", Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(fetchDockerfile))},
	} {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tw.Write([]byte(fetchDockerfile)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// fetchThroughExecutor fetch sunucusunu verilen politikayla kurar ve yürütücünün
// gerçek istemcisiyle bir kez çeker.
func fetchThroughExecutor(t *testing.T, f *fakeSvc, policy peercred.Policy) (kadranv1.FetchArchiveResponse_Decision, *os.File, error) {
	t.Helper()
	s, err := newFetchServerFor(f, policy)
	if err != nil {
		t.Fatal(err)
	}
	src := &kadranv1.GitSource{Host: "github.com", Owner: "octo", Repo: "app", CommitSha: goodSHA}
	return exec.NewGitFetcher(serve(t, s)).Fetch(context.Background(), src)
}

func selfPolicy() peercred.Policy {
	return peercred.Policy{AllowUIDs: []uint32{uint32(os.Getuid())}}
}

func noContextLeft(t *testing.T, dir string) {
	t.Helper()
	if left, _ := filepath.Glob(filepath.Join(dir, "kadran-context-*")); len(left) != 0 {
		t.Fatalf("build context left on disk: %v", left)
	}
}

func TestRealFetchServerArchiveReachesRealFetcher(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	f := &fakeSvc{decision: github.DecisionArchive, tarball: io.NopCloser(bytes.NewReader(codeloadTarGz(t)))}

	d, file, err := fetchThroughExecutor(t, f, selfPolicy())

	if d != kadranv1.FetchArchiveResponse_DECISION_ARCHIVE || file == nil || err != nil {
		t.Fatalf("Fetch = %v, %v, %v; want ARCHIVE with a file", d, file, err)
	}
	tr := tar.NewReader(file)
	hdr, err := tr.Next()
	if err != nil || hdr.Name != "Dockerfile" {
		t.Fatalf("first entry = %v, %v; want Dockerfile with the root stripped", hdr, err)
	}
	if body, _ := io.ReadAll(tr); string(body) != fetchDockerfile {
		t.Fatalf("Dockerfile = %q", body)
	}
	if _, err := tr.Next(); err != io.EOF {
		t.Fatalf("extra entry after Dockerfile: %v", err)
	}
	if f.decideCalls.Load() != 1 || f.tarCalls != 1 {
		t.Fatalf("decide=%d tarball=%d, want 1 and 1", f.decideCalls.Load(), f.tarCalls)
	}
	_ = file.Close()
	_ = os.Remove(file.Name())
	noContextLeft(t, tmp)
}

func TestRealFetchServerDecisionsWithoutArchive(t *testing.T) {
	for name, tc := range map[string]struct {
		decision github.Decision
		want     kadranv1.FetchArchiveResponse_Decision
	}{
		"not covered":   {github.DecisionNotCovered, kadranv1.FetchArchiveResponse_DECISION_NOT_COVERED},
		"not connected": {github.DecisionNotConnected, kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED},
	} {
		t.Run(name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("TMPDIR", tmp)
			f := &fakeSvc{decision: tc.decision}

			d, file, err := fetchThroughExecutor(t, f, selfPolicy())

			if d != tc.want || file != nil || err != nil {
				t.Fatalf("Fetch = %v, %v, %v; want %v", d, file, err, tc.want)
			}
			// Karar sunucudan geldi: el sıkışması reddedilip yürütücünün kendi
			// NOT_CONNECTED'ına düşülmedi.
			if f.decideCalls.Load() != 1 || f.tarCalls != 0 {
				t.Fatalf("decide=%d tarball=%d, want 1 and 0", f.decideCalls.Load(), f.tarCalls)
			}
			noContextLeft(t, tmp)
		})
	}
}

// Kontrol grubu: üretim politikası (yalnız root) root'suz arayanı el
// sıkışmada reddeder. Yürütücü bunu da NOT_CONNECTED görür, ama sunucu
// karar VERMEZ. decideCalls denetimi bu ikisini ayırıyor.
func TestRealFetchServerProductionPolicyRejectsNonRoot(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root: the production policy admits this caller")
	}
	f := &fakeSvc{decision: github.DecisionArchive}

	d, file, err := fetchThroughExecutor(t, f, fetchPolicy())

	if d != kadranv1.FetchArchiveResponse_DECISION_NOT_CONNECTED || file != nil || err != nil {
		t.Fatalf("Fetch = %v, %v, %v; want the pre-decision NOT_CONNECTED", d, file, err)
	}
	if f.decideCalls.Load() != 0 {
		t.Fatal("the server decided for a caller its policy rejects")
	}
}

//go:build linux

package exec

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/dockerdrv"
)

func TestPrivateGitFileOwnershipAndPermissions(t *testing.T) {
	if os.Geteuid() != 0 {
		// Bağımlılığın garanti olduğu ortamda (CI'ın sudo adımı) atlamak
		// yasak: değişken set iken test düşer, sessizce geçmez.
		if os.Getenv("KADRAN_TEST_REQUIRE_ROOT") == "1" {
			t.Fatal("root ownership check requires root (KADRAN_TEST_REQUIRE_ROOT=1)")
		}
		t.Skip("root ownership check needs root")
	}
	dir, err := os.MkdirTemp("/root", "kadran-credential-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(file, privateConfig(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPrivateGitCredentials(file, []string{"octocat/Hello-World"}); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []os.FileMode{0644, 0400, 0666} {
		if err := os.Chmod(file, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := readPrivateGitFile(file); err == nil {
			t.Fatalf("unsafe mode %o accepted", mode)
		}
	}
	if err := os.Chmod(file, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(file, link); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateGitFile(link); err == nil {
		t.Fatal("credential symlink accepted")
	}
	if err := os.Chown(file, 65534, 65534); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateGitFile(file); err == nil {
		t.Fatal("non-root credential accepted")
	}
	if err := os.Chown(file, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := readPrivateGitFile(file); err == nil {
		t.Fatal("writable parent accepted")
	}
}

func TestExecutorPrivateBuildUsesCredentialFreeUnixDockerContext(t *testing.T) {
	srv := newTestServer(t)
	socket := filepath.Join(t.TempDir(), "docker.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	dockerCalls := 0
	mock := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"ApiVersion":"1.48","MinAPIVersion":"1.44"}`))
			return
		}
		dockerCalls++
		data, _ := io.ReadAll(r.Body)
		if r.URL.Query().Has("remote") || strings.Contains(string(data), syntheticGitToken) || r.Header.Get("Authorization") != "" || r.Header.Get("Content-Type") != "application/x-tar" {
			t.Error("private credential crossed Docker boundary")
		}
		if !strings.Contains(string(data), "Dockerfile") {
			t.Error("archive body missing")
		}
		_, _ = w.Write([]byte(`{"aux":{"ID":"sha256:private-fixture"}}`))
	}))
	mock.Listener = listener
	mock.Start()
	defer mock.Close()
	srv.docker = dockerdrv.New(socket, "")
	srv.privateGit = configuredPrivateGit(t)
	srv.allowedRepos = []string{"octocat/Hello-World"}
	srv.privateGit.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) {
		return privateResponse(200, "", goodGitArchive(t)), nil
	})
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	stream := newBuildStream()
	if err := srv.ImageBuild(validBuildRequest(), stream); err != nil {
		t.Fatal(err)
	}
	if dockerCalls != 1 || len(stream.sent) < 1 || stream.sent[len(stream.sent)-1].GetImageId() != "sha256:private-fixture" {
		t.Fatal("private build did not produce positive image evidence")
	}
	files, _ := filepath.Glob(filepath.Join(temp, "kadran-context-*.tar"))
	if len(files) != 0 {
		t.Fatal("private build context left on disk")
	}
	if _, err := srv.privateGit.Context(context.Background(), nil); err == nil {
		t.Fatal("nil source accepted")
	}
}

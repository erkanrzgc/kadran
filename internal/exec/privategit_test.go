package exec

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/audit"
)

const syntheticGitToken = "synthetic_private_git_token"

func privateConfig() []byte {
	return []byte(`{"repositories":[{"repository":"octocat/Hello-World","token":"` + syntheticGitToken + `"}]}`)
}

func TestPrivateGitConfigurationIsRestrictedAndRedacted(t *testing.T) {
	allowed := []string{"octocat/Hello-World"}
	if _, err := parsePrivateGit(privateConfig(), allowed); err != nil {
		t.Fatalf("valid opt-in configuration failed: %v", err)
	}
	for _, tc := range []struct {
		name  string
		data  []byte
		allow []string
	}{
		{"mandatory allowlist", privateConfig(), nil},
		{"unlisted credential", privateConfig(), []string{"another/repo"}},
		{"unknown field", []byte(`{"repositories":[],"token":"` + syntheticGitToken + `"}`), allowed},
		{"invalid JSON secret", []byte(`{"repositories":[{"token":"` + syntheticGitToken + `"`), allowed},
		{"duplicate credential", []byte(`{"repositories":[{"repository":"octocat/Hello-World","token":"x"},{"repository":"OCTOCAT/hello-world","token":"y"}]}`), allowed},
		{"unsafe repository", []byte(`{"repositories":[{"repository":"../Hello-World","token":"x"}]}`), []string{"../Hello-World"}},
		{"empty token", []byte(`{"repositories":[{"repository":"octocat/Hello-World","token":""}]}`), allowed},
		{"header injection", []byte(`{"repositories":[{"repository":"octocat/Hello-World","token":"x\r\ny"}]}`), allowed},
		{"trailing JSON", append(privateConfig(), []byte(` {}`)...), allowed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parsePrivateGit(tc.data, tc.allow)
			if err == nil {
				t.Fatal("unsafe credential configuration accepted")
			}
			if strings.Contains(err.Error(), syntheticGitToken) {
				t.Fatal("credential appeared in parse error")
			}
		})
	}
}

type privateRoundTrip func(*http.Request) (*http.Response, error)

func (f privateRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func privateResponse(code int, location string, body []byte) *http.Response {
	return &http.Response{StatusCode: code, Header: http.Header{"Location": []string{location}}, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

func gitArchive(t *testing.T, entries ...*tar.Header) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	for _, entry := range entries {
		if err := tw.WriteHeader(entry); err != nil {
			t.Fatal(err)
		}
		if entry.Typeflag == tar.TypeReg && entry.Size > 0 {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(entry.Size))); err != nil {
				t.Fatal(err)
			}
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
func goodGitArchive(t *testing.T) []byte {
	return gitArchive(t, &tar.Header{Name: "octocat-Hello-World-7fd1a60/", Typeflag: tar.TypeDir, Mode: 0o755}, &tar.Header{Name: "octocat-Hello-World-7fd1a60/Dockerfile", Typeflag: tar.TypeReg, Mode: 0o644, Size: 12})
}
func configuredPrivateGit(t *testing.T) *PrivateGit {
	t.Helper()
	p, err := parsePrivateGit(privateConfig(), []string{"octocat/Hello-World"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPrivateArchiveKeepsPermanentTokenOutOfDockerContext(t *testing.T) {
	p := configuredPrivateGit(t)
	calls := 0
	p.client.Transport = privateRoundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if strings.Contains(r.URL.String(), syntheticGitToken) {
			t.Fatal("permanent credential put in URL")
		}
		if calls == 1 {
			if r.URL.String() != "https://api.github.com/repos/octocat/Hello-World/tarball/"+testSHA || r.Header.Get("Authorization") != "Bearer "+syntheticGitToken {
				t.Fatalf("wrong fixed authenticated archive request: %s", r.URL.Host)
			}
			return privateResponse(302, "https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/"+testSHA+"?token=temporary_signature", nil), nil
		}
		if r.Header.Get("Authorization") != "" {
			t.Fatal("permanent token forwarded to codeload")
		}
		return privateResponse(200, "", goodGitArchive(t)), nil
	})
	f, err := p.Context(context.Background(), validGitSource())
	if err != nil {
		t.Fatal(err)
	}
	defer closePrivateContext(f)
	tr := tar.NewReader(f)
	h, err := tr.Next()
	if err != nil || h.Name != "Dockerfile" {
		t.Fatalf("GitHub root directory not removed: %v, %v", h, err)
	}
	body, _ := io.ReadAll(tr)
	if strings.Contains(string(body), syntheticGitToken) || calls != 2 {
		t.Fatal("private credential crossed context boundary")
	}
}

func TestPrivateArchiveRejectsRedirectAndRepositoryEscapes(t *testing.T) {
	for _, location := range []string{
		"http://codeload.github.com/octocat/Hello-World/legacy.tar.gz/" + testSHA,
		"https://evil.example/octocat/Hello-World/legacy.tar.gz/" + testSHA,
		"https://user@codeload.github.com/octocat/Hello-World/legacy.tar.gz/" + testSHA,
		"https://codeload.github.com:444/octocat/Hello-World/legacy.tar.gz/" + testSHA,
		"https://codeload.github.com/octocat/another/legacy.tar.gz/" + testSHA,
		"https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/main",
		"https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/" + testSHA + "?token=x&token=y",
		"https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/" + testSHA + "?other=x",
		"https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/" + testSHA + "#secret",
	} {
		t.Run(location, func(t *testing.T) {
			p := configuredPrivateGit(t)
			calls := 0
			p.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) { calls++; return privateResponse(302, location, nil), nil })
			if _, err := p.Context(context.Background(), validGitSource()); err == nil {
				t.Fatal("unsafe redirect accepted")
			}
			if calls != 1 {
				t.Fatal("unsafe redirect fetched")
			}
		})
	}
	p := configuredPrivateGit(t)
	calls := 0
	p.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return privateResponse(200, "", goodGitArchive(t)), nil
	})
	src := validGitSource()
	src.Repo = "unlisted"
	if _, err := p.Context(context.Background(), src); err == nil || calls != 0 {
		t.Fatal("unknown repository reached credentials/network")
	}
}

func TestPrivateArchiveErrorsNeverExposeCredentialsAndDoNotFallback(t *testing.T) {
	for _, code := range []int{401, 403, 404, 500} {
		p := configuredPrivateGit(t)
		calls := 0
		p.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) {
			calls++
			return privateResponse(code, "", []byte(syntheticGitToken)), nil
		})
		_, err := p.Context(context.Background(), validGitSource())
		if err == nil || strings.Contains(err.Error(), syntheticGitToken) || calls != 1 {
			t.Fatalf("credential failure leaked or fell back: %v", err)
		}
	}
	p := configuredPrivateGit(t)
	p.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("transport leaked %s", syntheticGitToken)
	})
	if _, err := p.Context(context.Background(), validGitSource()); err == nil || strings.Contains(err.Error(), syntheticGitToken) {
		t.Fatalf("transport error leaked secret: %v", err)
	}
}

func TestPrivateArchiveRejectsUnsafeTarEntries(t *testing.T) {
	for _, entry := range []*tar.Header{
		{Name: "root/../outside", Typeflag: tar.TypeReg}, {Name: "/root/outside", Typeflag: tar.TypeReg}, {Name: "root/C:\\outside", Typeflag: tar.TypeReg},
		{Name: "root/link", Typeflag: tar.TypeSymlink, Linkname: "../../etc/passwd"}, {Name: "root/link", Typeflag: tar.TypeLink, Linkname: "../../etc/passwd"}, {Name: "root/device", Typeflag: tar.TypeChar},
	} {
		t.Run(entry.Name, func(t *testing.T) {
			p := configuredPrivateGit(t)
			p.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) {
				return privateResponse(200, "", gitArchive(t, entry)), nil
			})
			if f, err := p.Context(context.Background(), validGitSource()); err == nil {
				closePrivateContext(f)
				t.Fatal("unsafe archive accepted")
			}
		})
	}
}

func TestPrivateBuildCredentialFailureNeverFallsBackToDockerRemote(t *testing.T) {
	srv := newTestServer(t)
	p := configuredPrivateGit(t)
	calls := 0
	p.client.Transport = privateRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return privateResponse(401, "", []byte(syntheticGitToken)), nil
	})
	srv.privateGit = p
	srv.allowedRepos = []string{"octocat/Hello-World"}
	stream := newBuildStream()
	err := srv.ImageBuild(validBuildRequest(), stream)
	if err == nil || calls != 1 {
		t.Fatalf("private source was bypassed: calls=%d, err=%v", calls, err)
	}
	if strings.Contains(err.Error(), syntheticGitToken) {
		t.Fatal("credential failure exposed token through RPC")
	}
	records, err := srv.journal.Read(0, 100)
	if err != nil {
		t.Fatal(err)
	}
	last := records[len(records)-1]
	if last.Outcome != audit.OutcomeFailure || strings.Contains(last.ParamsJSON, syntheticGitToken) || strings.Contains(last.Detail, syntheticGitToken) {
		t.Fatalf("private build credential failure was not safely audited: %+v", last)
	}
}

func TestPrivateArchiveBoundsAndCorruption(t *testing.T) {
	archive := goodGitArchive(t)
	for _, tc := range []struct {
		name   string
		body   []byte
		limits archiveLimits
	}{
		{"compressed", archive, archiveLimits{int64(len(archive) - 1), 1 << 20, 100}},
		{"expanded", archive, archiveLimits{1 << 20, 600, 100}},
		{"entries", archive, archiveLimits{1 << 20, 1 << 20, 1}},
		{"truncated gzip", archive[:len(archive)-5], archiveLimits{1 << 20, 1 << 20, 100}},
		{"duplicate path", gitArchive(t, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}), archiveLimits{1 << 20, 1 << 20, 100}},
		{"multiple roots", gitArchive(t, &tar.Header{Name: "root/file", Typeflag: tar.TypeReg}, &tar.Header{Name: "other/file", Typeflag: tar.TypeReg}), archiveLimits{1 << 20, 1 << 20, 100}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := rewritePrivateArchive(context.Background(), bytes.NewReader(tc.body), io.Discard, tc.limits); err == nil {
				t.Fatal("unbounded or corrupt archive accepted")
			}
		})
	}
}

func TestPrivateGitCredentialRouting(t *testing.T) {
	p := configuredPrivateGit(t)
	src := validGitSource()
	if !p.hasCredential(src) {
		t.Fatal("configured repository lost private fetch")
	}
	src.Repo = "public"
	if p.hasCredential(src) {
		t.Fatal("public repository received private credentials")
	}
}

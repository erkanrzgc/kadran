package github

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

const testSHA = "7fd1a60b01f91b314f59955a4e4d4e80d8edf11d"

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(code int, header map[string]string, body []byte) *http.Response {
	h := http.Header{}
	for k, v := range header {
		h.Set(k, v)
	}
	return &http.Response{StatusCode: code, Header: h, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body))}
}

func codeloadURL(suffix string) string {
	return "https://codeload.github.com/octocat/Hello-World/legacy.tar.gz/" + testSHA + suffix
}

func TestTarballFollowsOnlyCodeloadRedirect(t *testing.T) {
	var seen []*http.Request
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r)
		if r.URL.Host == "api.github.com" {
			return respond(302, map[string]string{"Location": codeloadURL("?token=x")}, nil), nil
		}
		return respond(200, nil, []byte("archive-bytes")), nil
	})
	c := newClient(defaultEndpoints(), &http.Client{Transport: rt})
	rc, err := c.tarball(context.Background(), "octocat", "Hello-World", testSHA, "ghu_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	if body, _ := io.ReadAll(rc); string(body) != "archive-bytes" || len(seen) != 2 {
		t.Fatalf("body %q, %d requests", body, len(seen))
	}
	if seen[0].URL.String() != "https://api.github.com/repos/octocat/Hello-World/tarball/"+testSHA || seen[0].Header.Get("Authorization") != "Bearer ghu_SECRET" {
		t.Fatalf("wrong first request: %s", seen[0].URL)
	}
	if seen[1].Header.Get("Authorization") != "" {
		t.Fatal("token forwarded to codeload")
	}
}

func TestTarballRejectsOtherHostRedirect(t *testing.T) {
	calls := 0
	rt := roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return respond(302, map[string]string{"Location": "https://evil.example/octocat/Hello-World/legacy.tar.gz/" + testSHA}, nil), nil
	})
	c := newClient(defaultEndpoints(), &http.Client{Transport: rt})
	if _, err := c.tarball(context.Background(), "octocat", "Hello-World", testSHA, "ghu_SECRET"); err == nil || calls != 1 {
		t.Fatalf("redirect accepted or fetched: err=%v calls=%d", err, calls)
	}
}

func TestTarballErrorTextHasNoSignedURL(t *testing.T) {
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			return respond(302, map[string]string{"Location": codeloadURL("?token=SIGNED")}, nil), nil
		}
		return respond(500, nil, []byte("body SIGNED ghu_SECRET")), nil
	})
	c := newClient(defaultEndpoints(), &http.Client{Transport: rt})
	_, err := c.tarball(context.Background(), "octocat", "Hello-World", testSHA, "ghu_SECRET")
	if err == nil || strings.Contains(err.Error(), "SIGNED") || strings.Contains(err.Error(), "ghu_") {
		t.Fatalf("error leaks secret: %v", err)
	}
}

func TestTarballMapsAPIStatus(t *testing.T) {
	rt := roundTrip(func(*http.Request) (*http.Response, error) { return respond(404, nil, nil), nil })
	c := newClient(defaultEndpoints(), &http.Client{Transport: rt})
	if _, err := c.tarball(context.Background(), "octocat", "Hello-World", testSHA, "t"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestRedirectRules(t *testing.T) {
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
		if validArchiveRedirect(location, "octocat", "Hello-World", testSHA) {
			t.Errorf("unsafe redirect accepted: %s", location)
		}
	}
	for _, location := range []string{
		codeloadURL(""),
		codeloadURL("?token=temporary_signature"),
		"https://codeload.github.com:443/octocat/hello-world/legacy.tar.gz/" + testSHA,
	} {
		if !validArchiveRedirect(location, "octocat", "Hello-World", testSHA) {
			t.Errorf("valid redirect rejected: %s", location)
		}
	}
}

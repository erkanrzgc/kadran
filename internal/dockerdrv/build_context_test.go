package dockerdrv

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDockerRawContextIsStreamedAsTar(t *testing.T) {
	archive := "synthetic archive bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"ApiVersion":"1.48","MinAPIVersion":"1.44"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != archive || r.Header.Get("Content-Type") != "application/x-tar" {
			t.Errorf("local context was JSON encoded: body=%s, content-type=%s", body, r.Header.Get("Content-Type"))
		}
		_, _ = w.Write([]byte(`{"aux":{"ID":"sha256:test"}}`))
	}))
	defer srv.Close()
	c := &Client{http: srv.Client(), base: srv.URL}
	resp, err := c.do(context.Background(), http.MethodPost, "/build", nil, strings.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
}

func TestImageBuildUsesLocalContextWithoutRemoteURL(t *testing.T) {
	archive := "synthetic archive bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"ApiVersion":"1.48","MinAPIVersion":"1.44"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if r.URL.Query().Has("remote") || string(body) != archive || r.Header.Get("Content-Type") != "application/x-tar" {
			t.Errorf("local context escaped into Docker remote URL or was omitted: remote=%s, body=%s", r.URL.Query().Get("remote"), body)
		}
		_, _ = w.Write([]byte(`{"aux":{"ID":"sha256:test"}}`))
	}))
	defer srv.Close()
	c := &Client{http: srv.Client(), base: srv.URL}
	image, err := c.ImageBuild(context.Background(), BuildSpec{AppID: "blog", CommitSHA: strings.Repeat("a", 40), ContextTar: strings.NewReader(archive)}, func([]byte, bool) error { return nil })
	if err != nil || image != "sha256:test" {
		t.Fatalf("local image build failed: %s, %v", image, err)
	}
	if _, err := c.ImageBuild(context.Background(), BuildSpec{ContextURL: "https://github.com/u/r.git", ContextTar: strings.NewReader(archive)}, func([]byte, bool) error { return nil }); err == nil {
		t.Error("ambiguous local+remote context accepted")
	}
}

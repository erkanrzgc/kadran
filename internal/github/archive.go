package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var errArchiveRedirect = errors.New("github: archive redirect rejected")

// tarball, deponun bir commit'inin tar.gz arşivini açar. Kalıcı jeton yalnız
// api host'una gider; codeload'a yalnız kısa ömürlü imzalı adres ile gidilir.
func (c *client) tarball(ctx context.Context, owner, repo, sha, token string) (io.ReadCloser, error) {
	path := "/repos/" + owner + "/" + repo + "/tarball/" + sha
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ep.api+path, nil)
	if err != nil {
		return nil, errTransport
	}
	setAPIHeaders(req, token)
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusFound {
		location := resp.Header.Get("Location")
		_ = resp.Body.Close()
		if !validArchiveRedirect(location, owner, repo, sha) {
			return nil, errArchiveRedirect
		}
		resp, err = c.fetchSigned(ctx, location)
		if err != nil {
			return nil, err
		}
	}
	if err := statusError(resp); err != nil {
		_ = resp.Body.Close()
		return nil, err
	}
	return resp.Body, nil
}

// fetchSigned imzalı codeload adresini Authorization başlığı OLMADAN çeker.
func (c *client) fetchSigned(ctx context.Context, location string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, location, nil)
	if err != nil {
		return nil, errTransport
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	return c.send(req)
}

// validArchiveRedirect yalnız bu commit'in codeload adresini kabul eder.
func validArchiveRedirect(raw, owner, repo, sha string) bool {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 8192 || u.Scheme != "https" || u.Hostname() != "codeload.github.com" || (u.Port() != "" && u.Port() != "443") || u.User != nil || u.Fragment != "" || u.RawPath != "" || u.Opaque != "" {
		return false
	}
	expected := fmt.Sprintf("/%s/%s/legacy.tar.gz/%s", owner, repo, sha)
	if !strings.EqualFold(u.Path, expected) {
		return false
	}
	q, err := url.ParseQuery(u.RawQuery)
	return err == nil && (len(q) == 0 || (len(q) == 1 && len(q["token"]) == 1 && q.Get("token") != ""))
}

// Package github, kadran-git yetkisiz hizmetinin GitHub ile konuşan kısmıdır.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultLogin = "https://github.com"
	defaultAPI   = "https://api.github.com"

	maxFormBody = 64 << 10
	maxJSONBody = 1 << 20

	requestTimeout = 2 * time.Minute
)

// Durum koduna göre eşlenen sabit hatalar. Metinlere yanıt gövdesi, adres ya da
// sorgu metni asla girmez: imzalı codeload adresleri `?token=` taşır.
var (
	ErrUnauthorized = errors.New("github: unauthorized")
	ErrForbidden    = errors.New("github: access forbidden: the app lacks a permission or the repository is not installed")
	ErrNotFound     = errors.New("github: not found")
	ErrMoved        = errors.New("github: repository moved: update the app definition")
	ErrRateLimited  = errors.New("github: rate limited")

	errTransport = errors.New("github: transport failed")
	errBadPath   = errors.New("github: invalid request path")
	errResponse  = errors.New("github: invalid response")
	errTooLarge  = errors.New("github: response too large")
	// errUnprocessable HTTP 422'dir. Genel hatadan ayrı tutulur: commits uç
	// noktası olmayan bir dal için 404 değil 422 döner (gerçek GitHub, 17 Kas)
	// ve yalnız ResolveBranch onu ErrNotFound'a çevirir.
	errUnprocessable = errors.New("github: request failed (HTTP 422)")
)

// endpoints dışa açık değildir; testler dışında hep varsayılan sabitler kullanılır.
type endpoints struct{ login, api string }

func defaultEndpoints() endpoints { return endpoints{login: defaultLogin, api: defaultAPI} }

type client struct {
	http *http.Client
	ep   endpoints
	now  func() time.Time
}

// newClient, yönlendirmeleri hiçbir zaman kendiliğinden izlemeyen bir istemci
// kurar. h verilirse Transport'u korunur, çağıranın nesnesi değiştirilmez.
func newClient(ep endpoints, h *http.Client) *client {
	var hc http.Client
	if h == nil {
		hc = http.Client{Timeout: requestTimeout, Transport: &http.Transport{
			DialContext:           (&net.Dialer{Timeout: 10 * time.Second}).DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		}}
	} else {
		hc = *h
	}
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client{http: &hc, ep: ep, now: time.Now}
}

// send isteği gönderir; taşıma hatalarını sabit metne çevirir (URL hataları
// bearer ya da imzalı sorgu taşıyabilir).
func (c *client) send(req *http.Request) (*http.Response, error) {
	resp, err := c.http.Do(req)
	if err != nil {
		if ctxErr := req.Context().Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, errTransport
	}
	return resp, nil
}

// statusError 2xx dışındaki yanıtı sabit bir hataya çevirir; 2xx için nil döner.
func statusError(resp *http.Response) error {
	code := resp.StatusCode
	switch {
	case code >= 200 && code < 300:
		return nil
	case code == http.StatusUnauthorized:
		return ErrUnauthorized
	case code == http.StatusTooManyRequests,
		code == http.StatusForbidden && (resp.Header.Get("X-RateLimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""):
		return ErrRateLimited
	case code == http.StatusForbidden:
		return ErrForbidden
	case code == http.StatusNotFound:
		return ErrNotFound
	case code == http.StatusMovedPermanently, code == http.StatusTemporaryRedirect, code == http.StatusPermanentRedirect:
		return ErrMoved
	case code == http.StatusUnprocessableEntity:
		return errUnprocessable
	}
	return fmt.Errorf("github: request failed (HTTP %d)", code)
}

// decodeJSON gövdeyi sınırlı okuyup çözer; sınır aşılırsa ya da bozuksa sabit hata.
func decodeJSON(body io.Reader, out any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxJSONBody+1))
	if err != nil {
		return errTransport
	}
	if len(data) > maxJSONBody {
		return errTooLarge
	}
	if out == nil {
		return nil
	}
	if json.Unmarshal(data, out) != nil {
		return errResponse
	}
	return nil
}

// endpointURL sabit uç noktaya yol ekler; sonuç başka bir hosta çıkıyorsa
// reddeder. Hata metnine yol girmez.
func endpointURL(base, path string) (string, error) {
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "", errBadPath
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", errBadPath
	}
	u, err := url.Parse(base + path)
	if err != nil || u.Scheme != b.Scheme || u.Host != b.Host || u.User != nil {
		return "", errBadPath
	}
	return u.String(), nil
}

// postForm login uç noktasına form gövdesini (≤ 64 KiB) gönderir, JSON yanıtı outa çözer.
func (c *client) postForm(ctx context.Context, path string, form url.Values, out any) error {
	body := form.Encode()
	if len(body) > maxFormBody {
		return errTooLarge
	}
	full, err := endpointURL(c.ep.login, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, full, strings.NewReader(body))
	if err != nil {
		return errTransport
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.doJSON(req, out)
}

// getJSON api host'una GET atar; yanıt ≤ 1 MiB olmalı. token boşsa başlık eklenmez.
func (c *client) getJSON(ctx context.Context, path, token string, out any) error {
	full, err := endpointURL(c.ep.api, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return errTransport
	}
	setAPIHeaders(req, token)
	return c.doJSON(req, out)
}

// getRaw api host'una verilen Accept ile GET atar; gövde en çok limit bayt olabilir.
func (c *client) getRaw(ctx context.Context, path, token, accept string, limit int64) ([]byte, error) {
	full, err := endpointURL(c.ep.api, path)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, errTransport
	}
	setAPIHeaders(req, token)
	req.Header.Set("Accept", accept)
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, errTransport
	}
	if int64(len(data)) > limit {
		return nil, errTooLarge
	}
	return data, nil
}

func setAPIHeaders(req *http.Request, token string) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

func (c *client) doJSON(req *http.Request, out any) error {
	resp, err := c.send(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := statusError(resp); err != nil {
		return err
	}
	return decodeJSON(resp.Body, out)
}

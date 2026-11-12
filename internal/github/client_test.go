package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func serverClient(t *testing.T, h http.HandlerFunc) *client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newClient(endpoints{login: srv.URL, api: srv.URL}, nil)
}

func TestGetJSONRejectsOversizedBody(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`"` + strings.Repeat("a", 1<<20) + `"`))
	})
	var out string
	if err := c.getJSON(context.Background(), "/x", "", &out); err == nil {
		t.Fatal("oversized body accepted")
	}
}

func TestGetJSONAcceptsBodyAtLimit(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`"` + strings.Repeat("a", (1<<20)-2) + `"`))
	})
	var out string
	if err := c.getJSON(context.Background(), "/x", "", &out); err != nil || len(out) != (1<<20)-2 {
		t.Fatalf("body at limit rejected: %v", err)
	}
}

func TestPostFormRejectsOversizedForm(t *testing.T) {
	calls := 0
	c := serverClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	form := url.Values{"x": {strings.Repeat("a", 64<<10)}}
	if err := c.postForm(context.Background(), "/p", form, nil); err == nil || calls != 0 {
		t.Fatalf("oversized form sent: err=%v calls=%d", err, calls)
	}
}

func TestPostFormSendsAcceptAndDecodes(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" || r.FormValue("a") != "b" {
			t.Errorf("bad request: %v %v", r.Header, r.Form)
		}
		_, _ = w.Write([]byte(`{"k":"v"}`))
	})
	var out struct{ K string }
	if err := c.postForm(context.Background(), "/p", url.Values{"a": {"b"}}, &out); err != nil || out.K != "v" {
		t.Fatalf("postForm: %v %+v", err, out)
	}
}

func TestErrorTextNeverContainsToken(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"ghu_SECRET"}`))
	})
	err := c.getJSON(context.Background(), "/x", "ghu_SECRET", nil)
	if err == nil || strings.Contains(err.Error(), "ghu_") {
		t.Fatalf("error leaks token or is nil: %v", err)
	}
}

func TestTransportErrorTextHasNoURL(t *testing.T) {
	rt := roundTrip(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("dial " + r.URL.String() + " failed")
	})
	c := newClient(defaultEndpoints(), &http.Client{Transport: rt})
	err := c.getJSON(context.Background(), "/secret?token=abc", "ghu_SECRET", nil)
	if err == nil || strings.Contains(err.Error(), "api.github.com") || strings.Contains(err.Error(), "abc") {
		t.Fatalf("transport error leaks URL: %v", err)
	}
}

func TestMovedRepositoryIsReported(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "https://api.github.com/repositories/1")
		w.WriteHeader(301)
	})
	err := c.getJSON(context.Background(), "/x", "", nil)
	if !errors.Is(err, ErrMoved) || !strings.Contains(err.Error(), "repository moved: update the app definition") {
		t.Fatalf("moved not reported: %v", err)
	}
}

func TestRateLimitedIsDistinct(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(403)
	})
	if err := c.getJSON(context.Background(), "/x", "", nil); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("want ErrRateLimited, got %v", err)
	}
}

func TestStatusCodesMapToSentinels(t *testing.T) {
	for _, tc := range []struct {
		code   int
		header string
		want   error
	}{
		{401, "", ErrUnauthorized}, {403, "", ErrForbidden}, {404, "", ErrNotFound},
		{403, "Retry-After", ErrRateLimited}, {429, "", ErrRateLimited}, {429, "Retry-After", ErrRateLimited},
	} {
		c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) {
			if tc.header != "" {
				w.Header().Set(tc.header, "30")
			}
			w.WriteHeader(tc.code)
		})
		if err := c.getJSON(context.Background(), "/x", "", nil); !errors.Is(err, tc.want) {
			t.Errorf("HTTP %d %s: want %v, got %v", tc.code, tc.header, tc.want, err)
		}
	}
}

func TestForbiddenIsNotUnauthorized(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) })
	if err := c.getJSON(context.Background(), "/x", "", nil); errors.Is(err, ErrUnauthorized) {
		t.Fatalf("bare 403 must not be ErrUnauthorized: %v", err)
	}
}

func TestPathCannotEscapeHost(t *testing.T) {
	calls := 0
	c := serverClient(t, func(http.ResponseWriter, *http.Request) { calls++ })
	for _, path := range []string{"@evil.com/x", ".evil.com/x", "//evil.com/x", "x", ""} {
		if err := c.getJSON(context.Background(), path, "", nil); err == nil || strings.Contains(err.Error(), "evil") {
			t.Errorf("getJSON %q: err=%v", path, err)
		}
		if err := c.postForm(context.Background(), path, url.Values{"a": {"b"}}, nil); err == nil || strings.Contains(err.Error(), "evil") {
			t.Errorf("postForm %q: err=%v", path, err)
		}
	}
	if calls != 0 {
		t.Fatalf("%d requests sent", calls)
	}
}

func TestPathAllowsQuery(t *testing.T) {
	c := serverClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("per_page") != "100" {
			t.Errorf("query lost: %s", r.URL)
		}
		_, _ = w.Write([]byte(`{}`))
	})
	if err := c.getJSON(context.Background(), "/user/installations?per_page=100", "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestNewClientKeepsTransportAndNeverFollowsRedirects(t *testing.T) {
	calls := 0
	rt := roundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return respond(302, map[string]string{"Location": "https://evil.example/"}, nil), nil
	})
	orig := &http.Client{Transport: rt}
	c := newClient(defaultEndpoints(), orig)
	_ = c.getJSON(context.Background(), "/x", "", nil)
	if calls != 1 {
		t.Fatalf("redirect followed: %d calls", calls)
	}
	if orig.CheckRedirect != nil {
		t.Fatal("caller's client was mutated")
	}
}

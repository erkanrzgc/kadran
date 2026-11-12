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
	if err := c.postForm(context.Background(), c.ep.login+"/p", form, nil); err == nil || calls != 0 {
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
	if err := c.postForm(context.Background(), c.ep.login+"/p", url.Values{"a": {"b"}}, &out); err != nil || out.K != "v" {
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
	c := newClient(endpoints{api: "http://127.0.0.1:1"}, nil)
	err := c.getJSON(context.Background(), "/secret?token=abc", "ghu_SECRET", nil)
	if err == nil || strings.Contains(err.Error(), "127.0.0.1") || strings.Contains(err.Error(), "abc") {
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
	for code, want := range map[int]error{401: ErrUnauthorized, 403: ErrUnauthorized, 404: ErrNotFound, 429: ErrRateLimited} {
		c := serverClient(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) })
		if err := c.getJSON(context.Background(), "/x", "", nil); !errors.Is(err, want) {
			t.Errorf("HTTP %d: want %v, got %v", code, want, err)
		}
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

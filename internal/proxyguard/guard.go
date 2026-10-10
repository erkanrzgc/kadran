// Package proxyguard implements the control-plane's closed proxy capability.
package proxyguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
)

type Admin interface {
	Load(context.Context, *proxydrv.Config) error
	Current(context.Context) (*proxydrv.Config, error)
}

func Handler(a Admin) http.Handler {
	gate := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/routes" || r.URL.RawQuery != "" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var cfg *proxydrv.Config
		if r.Method == http.MethodPost {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, proxydrv.MaxRouteRequest))
			if err != nil {
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					fail(w, http.StatusRequestEntityTooLarge, "proxy route request too large")
				} else {
					fail(w, http.StatusBadRequest, "could not read proxy route request")
				}
				return
			}
			if err := uniqueJSON(body); err != nil {
				fail(w, http.StatusBadRequest, err.Error())
				return
			}
			dec := json.NewDecoder(bytes.NewReader(body))
			dec.DisallowUnknownFields()
			var set proxydrv.RouteSet
			if err := dec.Decode(&set); err != nil {
				fail(w, http.StatusBadRequest, "invalid proxy route request: "+err.Error())
				return
			}
			cfg, err = proxydrv.FixedConfig(set)
			if err != nil {
				fail(w, http.StatusBadRequest, err.Error())
				return
			}
		}
		// A full load and its semantic readback are one transaction. Concurrent
		// control-plane requests cannot overwrite one another's verification.
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-r.Context().Done():
			fail(w, http.StatusRequestTimeout, "proxy request canceled")
			return
		}
		if r.Context().Err() != nil {
			fail(w, http.StatusRequestTimeout, "proxy request canceled")
			return
		}
		if r.Method == http.MethodPost {
			if err := a.Load(r.Context(), cfg); err != nil {
				fail(w, http.StatusBadGateway, "proxy route load failed: "+err.Error())
				return
			}
		}
		var err error
		cfg, err = a.Current(r.Context())
		if err != nil {
			fail(w, http.StatusBadGateway, "proxy route read failed: "+err.Error())
			return
		}
		set, err := proxydrv.SafeRoutes(cfg)
		if err != nil {
			fail(w, http.StatusBadGateway, "unsafe live proxy configuration: "+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(set)
	})
}

func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// Go's JSON decoder normally accepts duplicate keys. Reject them as well as
// trailing documents so each security-relevant value has one interpretation.
func uniqueJSON(body []byte) error {
	d := json.NewDecoder(bytes.NewReader(body))
	var value func() error
	value = func() error {
		token, err := d.Token()
		if err != nil {
			return fmt.Errorf("invalid proxy JSON")
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return fmt.Errorf("invalid proxy JSON")
				}
				name, ok := key.(string)
				if !ok || seen[name] {
					return fmt.Errorf("duplicate or invalid proxy JSON field")
				}
				seen[name] = true
				if err := value(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := value(); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("invalid proxy JSON")
		}
		_, err = d.Token()
		return err
	}
	if err := value(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return fmt.Errorf("trailing proxy JSON data")
	}
	return nil
}

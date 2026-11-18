package main

import (
	"testing"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestActivatedNamesListeners(t *testing.T) {
	// Arrange
	getenv := env(map[string]string{
		"LISTEN_PID": "42", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "admin:fetch",
	})

	// Act
	fds, err := parseActivation(getenv, 42)

	// Assert
	if err != nil {
		t.Fatalf("parseActivation: %v", err)
	}
	if fds["admin"] != 3 || fds["fetch"] != 4 || len(fds) != 2 {
		t.Fatalf("fds = %v, want admin=3 fetch=4", fds)
	}
}

func TestActivatedNamesFollowOrder(t *testing.T) {
	fds, err := parseActivation(env(map[string]string{
		"LISTEN_PID": "7", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "fetch:admin",
	}), 7)
	if err != nil {
		t.Fatalf("parseActivation: %v", err)
	}
	if fds["fetch"] != 3 || fds["admin"] != 4 {
		t.Fatalf("fds = %v, want fetch=3 admin=4", fds)
	}
}

func TestActivatedRejectsForeignPID(t *testing.T) {
	_, err := parseActivation(env(map[string]string{
		"LISTEN_PID": "41", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "admin:fetch",
	}), 42)
	if err == nil {
		t.Fatal("foreign LISTEN_PID accepted")
	}
}

func TestActivatedRejectsMissingName(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"only admin":     {"LISTEN_PID": "42", "LISTEN_FDS": "1", "LISTEN_FDNAMES": "admin"},
		"only fetch":     {"LISTEN_PID": "42", "LISTEN_FDS": "1", "LISTEN_FDNAMES": "fetch"},
		"unknown name":   {"LISTEN_PID": "42", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "admin:other"},
		"duplicate":      {"LISTEN_PID": "42", "LISTEN_FDS": "2", "LISTEN_FDNAMES": "admin:admin"},
		"extra fd":       {"LISTEN_PID": "42", "LISTEN_FDS": "3", "LISTEN_FDNAMES": "admin:fetch:x"},
		"count mismatch": {"LISTEN_PID": "42", "LISTEN_FDS": "3", "LISTEN_FDNAMES": "admin:fetch"},
		"no names":       {"LISTEN_PID": "42", "LISTEN_FDS": "2"},
		"no fds":         {"LISTEN_PID": "42", "LISTEN_FDNAMES": "admin:fetch"},
		"no pid":         {"LISTEN_FDS": "2", "LISTEN_FDNAMES": "admin:fetch"},
		"bad count":      {"LISTEN_PID": "42", "LISTEN_FDS": "x", "LISTEN_FDNAMES": "admin:fetch"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseActivation(env(m), 42); err == nil {
				t.Fatalf("accepted %v", m)
			}
		})
	}
}

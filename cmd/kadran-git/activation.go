package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

const (
	socketAdmin = "admin"
	socketFetch = "fetch"
	// firstFD, systemd'nin devrettiği ilk dosya tanıtıcısıdır (SD_LISTEN_FDS_START).
	firstFD = 3
)

// parseActivation systemd soket etkinleştirmesini ad -> fd olarak çözer.
// Saf işlevdir: LISTEN_PID bu süreç olmalı, LISTEN_FDS ad sayısına eşit olmalı
// ve adlar tam olarak {admin, fetch} kümesi olmalı (eksik, çift, bilinmeyen: hata).
func parseActivation(getenv func(string) string, pid int) (map[string]int, error) {
	if p, err := strconv.Atoi(getenv("LISTEN_PID")); err != nil || p != pid {
		return nil, fmt.Errorf("LISTEN_PID does not match this process")
	}
	n, err := strconv.Atoi(getenv("LISTEN_FDS"))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("LISTEN_FDS is missing or invalid")
	}
	raw := getenv("LISTEN_FDNAMES")
	if raw == "" {
		return nil, fmt.Errorf("LISTEN_FDNAMES is missing")
	}
	names := strings.Split(raw, ":")
	if len(names) != n {
		return nil, fmt.Errorf("LISTEN_FDS (%d) does not match LISTEN_FDNAMES (%d names)", n, len(names))
	}
	fds := make(map[string]int, n)
	for i, name := range names {
		if name != socketAdmin && name != socketFetch {
			return nil, fmt.Errorf("unexpected socket name %q", name)
		}
		if _, dup := fds[name]; dup {
			return nil, fmt.Errorf("duplicate socket name %q", name)
		}
		fds[name] = firstFD + i
	}
	if len(fds) != 2 {
		return nil, fmt.Errorf("both %q and %q sockets are required", socketAdmin, socketFetch)
	}
	return fds, nil
}

// activated, systemd'den gelen iki unix dinleyiciyi adlarıyla döner.
func activated(getenv func(string) string, pid int) (map[string]net.Listener, error) {
	fds, err := parseActivation(getenv, pid)
	if err != nil {
		return nil, err
	}
	out := make(map[string]net.Listener, len(fds))
	for name, fd := range fds {
		ln, err := listenerFromFD(fd, name)
		if err != nil {
			for _, l := range out {
				_ = l.Close()
			}
			return nil, err
		}
		out[name] = ln
	}
	return out, nil
}

// unsetActivationEnv etkinleştirme değişkenlerini alt süreçlerden saklar.
func unsetActivationEnv() {
	for _, k := range []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"} {
		_ = os.Unsetenv(k)
	}
}

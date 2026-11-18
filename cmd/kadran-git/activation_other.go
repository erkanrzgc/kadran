//go:build !linux

package main

import (
	"errors"
	"net"
)

// listenerFromFD Linux dışında desteklenmez (systemd yalnız Linux'ta).
func listenerFromFD(int, string) (net.Listener, error) {
	return nil, errors.New("socket activation is only supported on Linux")
}

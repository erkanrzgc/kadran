//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// listenerFromFD fd'yi CLOEXEC yapar ve unix dinleyiciye sarar.
func listenerFromFD(fd int, name string) (net.Listener, error) {
	syscall.CloseOnExec(fd)
	f := os.NewFile(uintptr(fd), "kadran-git-"+name)
	if f == nil {
		return nil, fmt.Errorf("socket %q: invalid file descriptor", name)
	}
	defer func() { _ = f.Close() }() // FileListener fd'yi çoğaltır
	ln, err := net.FileListener(f)
	if err != nil {
		return nil, fmt.Errorf("socket %q: %w", name, err)
	}
	if _, ok := ln.(*net.UnixListener); !ok {
		_ = ln.Close()
		return nil, fmt.Errorf("socket %q is not a unix listener", name)
	}
	return ln, nil
}

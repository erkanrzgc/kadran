// kadran-proxy is a rootless capability broker. Only this identity can reach
// Caddy's raw admin socket; the daemon can submit validated typed routes.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/erkanrzgc/kadran/internal/proxydrv"
	"github.com/erkanrzgc/kadran/internal/proxyguard"
)

func main() {
	if err := run(); err != nil {
		slog.Error("proxy broker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// No free-form addresses or configuration flags. systemd owns the socket
	// and passes its already permissioned listener as descriptor 3.
	if len(os.Args) != 1 {
		return errors.New("proxy broker accepts no arguments")
	}
	file := os.NewFile(3, "proxy.socket")
	if file == nil {
		return errors.New("proxy socket descriptor missing")
	}
	ln, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		return err
	}
	defer func() { _ = ln.Close() }()
	if _, ok := ln.Addr().(*net.UnixAddr); !ok {
		return errors.New("proxy listener must be a Unix socket")
	}
	server := &http.Server{Handler: proxyguard.Handler(proxydrv.NewAdmin("/run/kadran-caddy/admin.sock")), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8 << 10}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ln) }()
	select {
	case err := <-done:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdown)
	}
}

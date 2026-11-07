package main

import (
	"context"
	"fmt"
	"time"

	"github.com/erkanrzgc/kadran/internal/sidecar"
)

// runSidecar, stdio üzerinde JSON-RPC sunucusu çalıştırır. Yöntemler ve
// çerçeveleme internal/sidecar'da; burada yalnız CLI'ın verdikleri
// (oturum, sunucu listesi, dal çözümü, uyarı) bağlanır.
func (c *cli) runSidecar(ctx context.Context, args []string) int {
	fs := c.newFlagSet("sidecar")
	timeout := fs.Duration("timeout", defaultTimeout, "time limit for a single call")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() > 0 {
		return c.usageError("`sidecar` takes no arguments")
	}

	d := sidecar.New(c.sidecarDeps(*timeout))
	defer d.Close()

	if err := sidecar.Serve(ctx, d, c.stdin, c.stdout); err != nil {
		return c.fail(err)
	}
	return exitOK
}

// sidecarDeps, dağıtıcının CLI'dan aldıkları.
func (c *cli) sidecarDeps(timeout time.Duration) sidecar.Deps {
	return sidecar.Deps{
		CallTimeout:   timeout,
		Servers:       c.serverStore(),
		LoadSession:   c.loadSession,
		ResolveCommit: c.resolveCommit,
		Warn: func(message string) {
			if c.stderr != nil {
				fmt.Fprintln(c.stderr, progName+": warning:", message)
			}
		},
	}
}

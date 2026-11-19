package cli

import (
	"context"
	"io"
)

// Env, bir komut calistirmasinin giris/cikis akislarini tasir. Surec
// kurulumu (cmd/kadran) gercek akislari verir; masaustu konsolu kendi
// yakalayicilarini.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Run, args ile verilen komutu calistirir ve cikis kodunu dondurur.
func Run(ctx context.Context, args []string, env Env) int {
	c := &cli{stdin: env.Stdin, stdout: env.Stdout, stderr: env.Stderr}
	return c.run(ctx, args)
}

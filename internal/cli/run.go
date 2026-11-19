package cli

import (
	"context"
	"io"
	"os"
	"os/exec"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Env, bir komut calistirmasinin giris/cikis akislarini tasir. Surec
// kurulumu (cmd/kadran) gercek akislari verir; masaustu konsolu kendi
// yakalayicilarini. Akislar disindaki alanlar istege bagli kancalardir:
// nil olan, bugunku davranisi (CLI ciktisi bayt bayt ayni) birakir.
type Env struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	// Sensitive, yalnizca TOTP sirrini, otpauth adresini, QR'i ve kurtarma
	// kodlarini (basligiyla) alir. nil ise Stdout.
	Sensitive io.Writer
	// Dial, sunucuya baglanir (nil: gercek baglanti).
	Dial func(ctx context.Context, rawTarget string) (*client.Client, *kadranv1.PingResponse, error)
	// Command, `ssh` baslatan HER yerden gecer (nil: exec.CommandContext).
	Command func(ctx context.Context, name string, args ...string) *exec.Cmd
	// Getenv ve UserHomeDir, ortam okumalari (nil: os.Getenv / os.UserHomeDir).
	Getenv      func(string) string
	UserHomeDir func() (string, error)
}

// newCLI, Env'den komut calistiricisini kurar.
func newCLI(env Env) *cli {
	return &cli{
		stdin: env.Stdin, stdout: env.Stdout, stderr: env.Stderr,
		sensitiveOut: env.Sensitive, dial: env.Dial, command: env.Command,
		getenvFn: env.Getenv, homeDirFn: env.UserHomeDir,
	}
}

// Run, args ile verilen komutu calistirir ve cikis kodunu dondurur.
func Run(ctx context.Context, args []string, env Env) int {
	return newCLI(env).run(ctx, args)
}

// sensitive, gizli ciktinin yazildigi akis.
func (c *cli) sensitive() io.Writer {
	if c.sensitiveOut != nil {
		return c.sensitiveOut
	}
	return c.stdout
}

func (c *cli) getenv(key string) string {
	if c.getenvFn != nil {
		return c.getenvFn(key)
	}
	return os.Getenv(key)
}

func (c *cli) homeDir() (string, error) {
	if c.homeDirFn != nil {
		return c.homeDirFn()
	}
	return os.UserHomeDir()
}

// sshCmd, `ssh` baslatan cli-ici yerlerin tek kapisi: Command kancasi
// varsa o, yoksa exec.CommandContext.
func (c *cli) sshCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	if c.command != nil {
		return c.command(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...) //nolint:gosec // sabit program, kabuksuz argv
}

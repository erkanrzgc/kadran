// kadran, Kadran'ın iş istasyonu komut satırı aracıdır.
//
// Sunucuya iki yoldan bağlanır:
//
//   - SSH:   `kadran status kullanici@sunucu` — `ssh` alt süreci üzerinden
//   - Yerel: `kadran status` — sunucunun kendisinde /run/kadran/api.sock
//
// # Anahtar malzemesi bu programa girmez
//
// Parola sorulmaz, özel anahtar okunmaz. Kimlik doğrulamayı `ssh` yapar;
// anahtar ssh-agent'ta veya ~/.ssh altındadır ve kadran onu hiç görmez.
// BatchMode=yes ile çalıştığı için bir istem de asla açılmaz.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/erkanrzgc/kadran/internal/cli"
)

func main() {
	// SIGINT/SIGTERM bağlamı iptal eder: ssh alt süreci de böylece
	// toplanır ve arkada yetim bir bağlantı kalmaz.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Run(ctx, os.Args[1:], cli.Env{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr}))
}

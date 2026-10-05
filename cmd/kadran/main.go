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
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/erkanrzgc/kadran/internal/bootstrap"
	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/domaincheck"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/version"
)

// Çıkış kodları. Betikler ve cron bunlara bakar.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	// exitChainInvalid, denetim zincirinin KIRIK olduğunu bildirir.
	//
	// "Doğrulanamadı"dan (exitError) ayrı bir kod olması kasıtlıdır:
	// cron'a konulan bir `kadran audit verify`, executor'ın kapalı
	// olduğu durumla kurcalama şüphesini karıştırmamalı.
	exitChainInvalid = 3
)

// defaultTimeout, tek bir komutun tamamlanması için tanınan süre.
//
// SSH el sıkışması yavaş bir ağda birkaç saniye sürebilir; ssh'ın kendi
// ConnectTimeout'u 10 saniye. 30 saniye ikisini de kapsar ve donmuş bir
// komutun süresiz asılı kalmasını engeller.
const defaultTimeout = 30 * time.Second

// cli, giriş/çıkış akışlarını taşır. Testlerin çıktıyı yakalayabilmesi
// için os.Stdout'a doğrudan yazılmıyor.
// progName, iş istasyonu aracının adı: hata öneki, kullanım ve sürüm
// satırı. Sunucu tarafının adları (kadrand, kadran-client, ...) v0.4.0'da
// aynı ada geçti (K-136).
const progName = "kadran"

type cli struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// Testlerin değiştirdiği bağımlılıklar; nil ise gerçekleri kullanılır.
	resolver    domaincheck.Resolver
	sshHostname func(ctx context.Context, host string) (string, error)
	dial        func(ctx context.Context, rawTarget string) (*client.Client, *kadranv1.PingResponse, error)
	// `domain check`: alan adının portlarına bağlanma ve güvenilen kökler
	// (nil: sistemin kökleri).
	dialNet  func(ctx context.Context, network, addr string) (net.Conn, error)
	tlsRoots *x509.CertPool
	// `key`: sunucudaki anahtar işlemleri (nil: gerçek ssh).
	keys keyManager
}

func main() {
	// SIGINT/SIGTERM bağlamı iptal eder: ssh alt süreci de böylece
	// toplanır ve arkada yetim bir bağlantı kalmaz.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := &cli{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	os.Exit(c.run(ctx, os.Args[1:]))
}

type command struct {
	name    string
	args    string
	summary string
	run     func(c *cli, ctx context.Context, args []string) int
}

func commands() []command {
	return []command{
		{"status", "[target]", "shows server and daemon status", (*cli).runStatus},
		{"app", "<create|update|list|show|delete> …", "manages app definitions", (*cli).runApp},
		{"deploy", "<app> [target]", "builds a commit and switches traffic to it", (*cli).runDeploy},
		{"rollback", "<app> [target]", "switches traffic back to the previous release", (*cli).runRollback},
		{"logs", "[-f] <app> [target]", "streams the live release's output", (*cli).runLogs},
		{"prune", "[-dry-run] <app>|-all [target]", "removes old releases' containers", (*cli).runPrune},
		{"alarms", "[target]", "lists active fault conditions", (*cli).runAlarms},
		{"backup", "<create|list> [target]", "takes and lists database backups", (*cli).runBackup},
		{"domain", "check <domain> [target]", "checks a domain's DNS, ports and certificate", (*cli).runDomain},
		{"audit", "<list|verify> [target]", "reads and verifies the audit chain", (*cli).runAudit},
		{"sidecar", "", "stdio JSON-RPC server for the desktop app", (*cli).runSidecar},
		{"bootstrap", "root@server | -sudo user@server", "installs or upgrades the server", (*cli).runBootstrap},
		{"key", "<list|add|remove> … root@server", "manages deploy-only keys", (*cli).runKey},
		{"version", "", "prints version information", (*cli).runVersion},
	}
}

func (c *cli) run(ctx context.Context, args []string) int {
	if len(args) == 0 {
		c.usage()
		return exitUsage
	}

	name := args[0]
	if name == "-h" || name == "--help" || name == "help" {
		c.usage()
		return exitOK
	}

	for _, cmd := range commands() {
		if cmd.name == name {
			return cmd.run(c, ctx, args[1:])
		}
	}

	fmt.Fprintf(c.stderr, progName+": unknown command %q\n\n", name)
	c.usage()
	return exitUsage
}

func (c *cli) usage() {
	fmt.Fprintf(c.stderr, `kadran %s — Kadran workstation tool

Usage:
  kadran <command> [options] [target]

Commands:
`, version.Version)

	tw := tabwriter.NewWriter(c.stderr, 0, 0, 2, ' ', 0)
	for _, cmd := range commands() {
		fmt.Fprintf(tw, "  %s %s\t%s\n", cmd.name, cmd.args, cmd.summary)
	}
	_ = tw.Flush()

	fmt.Fprintf(c.stderr, `
Target forms:
  (empty)                   local socket — %s
  /path/api.sock            local socket, explicit path
  user@server               SSH (default user: %s)
  user@server:2222          SSH, custom port
  server                    SSH, default user

Exit codes:
  %d  success
  %d  error (could not connect, chain could not be verified)
  %d  usage error
  %d  audit chain BROKEN — possible tampering

Options go after the command and before the target:
  kadran audit list --limit 20 user@server
`, client.DefaultSocketPath, client.DefaultSSHUser,
		exitOK, exitError, exitUsage, exitChainInvalid)
}

// newFlagSet, komutlar için ortak davranışlı bir bayrak kümesi kurar.
//
// ContinueOnError kullanılıyor: flag paketinin varsayılanı os.Exit çağırır
// ve bu, çıkış kodunu tek bir yerden yönetmeyi imkânsız kılardı.
func (c *cli) newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	return fs
}

// connect, hedefi çözümler, bağlanır ve protokol uyumunu doğrular.
//
// Protokol denetimi ilk iş olarak yapılır: uyumsuz sürümlerle konuşup
// yarı anlaşılmış yanıtlar üretmektense hemen durmak daha güvenli.
func (c *cli) connect(ctx context.Context, rawTarget string) (*client.Client, *kadranv1.PingResponse, error) {
	if c.dial != nil {
		return c.dial(ctx, rawTarget)
	}
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return nil, nil, err
	}

	if !target.IsLocal() && !client.SSHAvailable() {
		return nil, nil, errors.New(
			"`ssh` not found — the OpenSSH client is needed to reach a remote server")
	}

	conn, err := client.Dial(target)
	if err != nil {
		return nil, nil, err
	}

	ping, err := conn.CheckProtocol(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	return conn, ping, nil
}

// fail, hatayı stderr'e yazar ve hata çıkış kodu döndürür.
func (c *cli) fail(err error) int {
	fmt.Fprintln(c.stderr, progName+":", err)
	return exitError
}

// usageError, kullanım hatasını bildirir.
func (c *cli) usageError(format string, args ...any) int {
	fmt.Fprintf(c.stderr, progName+": "+format+"\n", args...)
	return exitUsage
}

func (c *cli) runVersion(_ context.Context, args []string) int {
	if len(args) > 0 {
		return c.usageError("`version` takes no arguments")
	}
	fmt.Fprintf(c.stdout, progName+" %s (%s)\nprotocol %d\n",
		version.Version, version.Commit, version.Protocol)
	return exitOK
}

// runBootstrap, sunucuyu kurar ya da yükseltir.
//
// Root yetkisi isteyen TEK komut bu. Yetki ya root'a SSH ile ya da -sudo
// kipinde hedef kullanıcının PAROLASIZ sudo'suyla alınıyor (K-122); ikinci
// yol, root'a SSH'ı kapalı getiren bulut imajları için. Kurulumdan sonra
// günlük kullanım yetkisiz `kadran-client` üzerinden yürür.
func (c *cli) runBootstrap(ctx context.Context, args []string) int {
	fs := c.newFlagSet("bootstrap")
	binaryDir := fs.String("binaries", defaultBinaryDir(), "directory holding the linux binaries")
	repoRoot := fs.String("repo", ".", "repository root to read the systemd units from")
	clientKey := fs.String("client-key", defaultClientKey(), "PUBLIC key to authorize on the server")
	// 30 dakika: kurulum paketi ~75 MiB. Taze sunucu testinde (K-112)
	// aynı ev hattından bir koşu 5 dakikada yalnızca 28 MB gönderdi ve 10
	// dakikalık eski sınır kurulumu yükleme bitmeden kesti; aynı gün başka
	// bir koşu yüklemeyi bir dakikadan kısa sürede bitirdi.
	timeout := fs.Duration("timeout", 30*time.Minute, "overall time limit")
	sudo := fs.Bool("sudo", false,
		"install through the target user's PASSWORDLESS sudo instead of SSH as root (never asks for a password)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		return c.usageError("usage: kadran bootstrap [options] root@server  |  kadran bootstrap -sudo user@server")
	}

	target, err := client.ParseTarget(fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	if target.IsLocal() {
		return c.usageError("`bootstrap` needs a remote target, not a local socket")
	}
	if !client.SSHAvailable() {
		return c.fail(errors.New("`ssh` not found — the OpenSSH client is required"))
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	fmt.Fprintf(c.stdout, "Kadran install — %s\n", target.String())
	fmt.Fprintln(c.stdout,
		"No password or private key is asked for; `ssh` does the authentication.")

	err = bootstrap.Run(ctx, bootstrap.Options{
		Host:          target.SSHUser + "@" + target.SSHHost,
		Port:          target.SSHPort,
		BinaryDir:     *binaryDir,
		RepoRoot:      *repoRoot,
		ClientKeyPath: *clientKey,
		Sudo:          *sudo,
		Stdout:        c.stdout,
		Stderr:        c.stderr,
	})
	if err != nil {
		return c.fail(err)
	}

	fmt.Fprintf(c.stdout, "\nTo verify:\n  kadran status %s@%s\n",
		client.DefaultSSHUser, target.SSHHost)
	return exitOK
}

// defaultBinaryDir, derlenmiş linux binary'lerinin varsayılan yeri.
func defaultBinaryDir() string {
	if dir := os.Getenv("KADRAN_BINARY_DIR"); dir != "" {
		return dir
	}
	return "bin"
}

// defaultClientKey, iş istasyonunun varsayılan açık anahtarı.
func defaultClientKey() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ssh", "id_ed25519.pub")
}

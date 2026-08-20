// panely, Panely'nin iş istasyonu komut satırı aracıdır.
//
// Sunucuya iki yoldan bağlanır:
//
//   - SSH:   `panely status kullanici@sunucu` — `ssh` alt süreci üzerinden
//   - Yerel: `panely status` — sunucunun kendisinde /run/panely/api.sock
//
// # Anahtar malzemesi bu programa girmez
//
// Parola sorulmaz, özel anahtar okunmaz. Kimlik doğrulamayı `ssh` yapar;
// anahtar ssh-agent'ta veya ~/.ssh altındadır ve panely onu hiç görmez.
// BatchMode=yes ile çalıştığı için bir istem de asla açılmaz.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/erkanrzgc/panely/internal/bootstrap"
	"github.com/erkanrzgc/panely/internal/client"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
	"github.com/erkanrzgc/panely/internal/version"
)

// Çıkış kodları. Betikler ve cron bunlara bakar.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2

	// exitChainInvalid, denetim zincirinin KIRIK olduğunu bildirir.
	//
	// "Doğrulanamadı"dan (exitError) ayrı bir kod olması kasıtlıdır:
	// cron'a konulan bir `panely audit verify`, executor'ın kapalı
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
type cli struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
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
		{"status", "[hedef]", "sunucu ve daemon durumunu gösterir", (*cli).runStatus},
		{"app", "<create|list|show> …", "uygulama tanımlarını yönetir", (*cli).runApp},
		{"deploy", "<uygulama> [hedef]", "bir commit'i derler ve trafiği ona çevirir", (*cli).runDeploy},
		{"rollback", "<uygulama> [hedef]", "trafiği bir önceki sürüme geri çevirir", (*cli).runRollback},
		{"audit", "<list|verify> [hedef]", "denetim zincirini okur ve doğrular", (*cli).runAudit},
		{"sidecar", "", "Electron için stdio JSON-RPC sunucusu", (*cli).runSidecar},
		{"bootstrap", "root@sunucu", "sunucuyu sıfırdan kurar (tek seferlik)", (*cli).runBootstrap},
		{"version", "", "sürüm bilgisini yazar", (*cli).runVersion},
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

	fmt.Fprintf(c.stderr, "panely: bilinmeyen komut %q\n\n", name)
	c.usage()
	return exitUsage
}

func (c *cli) usage() {
	fmt.Fprintf(c.stderr, `panely %s — Panely iş istasyonu aracı

Kullanım:
  panely <komut> [seçenekler] [hedef]

Komutlar:
`, version.Version)

	tw := tabwriter.NewWriter(c.stderr, 0, 0, 2, ' ', 0)
	for _, cmd := range commands() {
		fmt.Fprintf(tw, "  %s %s\t%s\n", cmd.name, cmd.args, cmd.summary)
	}
	_ = tw.Flush()

	fmt.Fprintf(c.stderr, `
Hedef biçimleri:
  (boş)                     yerel soket — %s
  /yol/api.sock             yerel soket, açık yol
  kullanici@sunucu          SSH (varsayılan kullanıcı: %s)
  kullanici@sunucu:2222     SSH, özel port
  sunucu                    SSH, varsayılan kullanıcı

Çıkış kodları:
  %d  başarılı
  %d  hata (bağlantı kurulamadı, zincir doğrulanamadı)
  %d  kullanım hatası
  %d  denetim zinciri KIRIK — kurcalama şüphesi

Seçenekler komuttan sonra, hedeften önce gelir:
  panely audit list --limit 20 kullanici@sunucu
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
func (c *cli) connect(ctx context.Context, rawTarget string) (*client.Client, *panelyv1.PingResponse, error) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return nil, nil, err
	}

	if !target.IsLocal() && !client.SSHAvailable() {
		return nil, nil, errors.New(
			"`ssh` komutu bulunamadı — uzak sunucuya bağlanmak için OpenSSH istemcisi gerekli")
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
	fmt.Fprintln(c.stderr, "panely:", err)
	return exitError
}

// usageError, kullanım hatasını bildirir.
func (c *cli) usageError(format string, args ...any) int {
	fmt.Fprintf(c.stderr, "panely: "+format+"\n", args...)
	return exitUsage
}

func (c *cli) runVersion(_ context.Context, args []string) int {
	if len(args) > 0 {
		return c.usageError("`version` argüman almaz")
	}
	fmt.Fprintf(c.stdout, "panely %s (%s)\nprotokol %d\n",
		version.Version, version.Commit, version.Protocol)
	return exitOK
}

// runBootstrap, sunucuyu sıfırdan kurar.
//
// Bu komut root olarak bağlanan TEK komuttur ve yalnızca bir kez
// çalıştırılır. Kurulum bittikten sonra günlük kullanım yetkisiz
// `panely-client` kullanıcısı üzerinden yürür; root erişimine bir daha
// gerek kalmaz.
func (c *cli) runBootstrap(ctx context.Context, args []string) int {
	fs := c.newFlagSet("bootstrap")
	binaryDir := fs.String("binaries", defaultBinaryDir(), "linux binary'lerinin bulunduğu dizin")
	repoRoot := fs.String("repo", ".", "systemd birimlerinin okunacağı depo kökü")
	clientKey := fs.String("client-key", defaultClientKey(), "sunucuya yetkilendirilecek AÇIK anahtar")
	timeout := fs.Duration("timeout", 10*time.Minute, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		return c.usageError("kullanım: panely bootstrap [seçenekler] root@sunucu")
	}

	target, err := client.ParseTarget(fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	if target.IsLocal() {
		return c.usageError("`bootstrap` uzak bir hedef ister, yerel soket değil")
	}
	if !client.SSHAvailable() {
		return c.fail(errors.New("`ssh` komutu bulunamadı — OpenSSH istemcisi gerekli"))
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	fmt.Fprintf(c.stdout, "Panely kurulumu — %s\n", target.String())
	fmt.Fprintln(c.stdout,
		"Parola veya özel anahtar istenmez; kimlik doğrulamayı `ssh` yapar.")

	err = bootstrap.Run(ctx, bootstrap.Options{
		Host:          target.SSHUser + "@" + target.SSHHost,
		Port:          target.SSHPort,
		BinaryDir:     *binaryDir,
		RepoRoot:      *repoRoot,
		ClientKeyPath: *clientKey,
		Stdout:        c.stdout,
		Stderr:        c.stderr,
	})
	if err != nil {
		return c.fail(err)
	}

	fmt.Fprintf(c.stdout, "\nDoğrulamak için:\n  panely status %s@%s\n",
		client.DefaultSSHUser, target.SSHHost)
	return exitOK
}

// defaultBinaryDir, derlenmiş linux binary'lerinin varsayılan yeri.
func defaultBinaryDir() string {
	if dir := os.Getenv("PANELY_BINARY_DIR"); dir != "" {
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

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/erkanrzgc/kadran/internal/bootstrap"
	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/connproto"
)

// ── `kadran key`: yalnızca dağıtım yapabilen anahtarlar (K-131) ──────
//
// Rol authorized_keys satırında durduğu ve o dosyaya yalnız root yazdığı
// için bu komut bootstrap'ın yetki yolunu kullanıyor: root@sunucu ya da
// -sudo kullanıcı@sunucu. kadran-client ile ÇALIŞMAZ (zorlanmış komut).

const keyUsage = "kullanım:\n" +
	"  kadran key list   [-sudo] root@sunucu\n" +
	"  kadran key add    -deploy uyg1,uyg2 [-name ad] [-sudo] <anahtar.pub> root@sunucu\n" +
	"  kadran key remove [-sudo] <SHA256:parmak-izi> root@sunucu"

// keyTimeout, bir anahtar işleminin üst sınırı: üç kısa uzak komut.
const keyTimeout = 2 * time.Minute

// maxPublicKeyFile, okunacak açık anahtar dosyasının üst sınırı. En büyük
// RSA açık anahtarı birkaç KB; daha büyüğü yanlış dosyadır.
const maxPublicKeyFile = 16 << 10

// keyManager, sunucudaki anahtar işlemleri; testler sahtesini koyuyor.
type keyManager interface {
	List(ctx context.Context, opts bootstrap.KeyOptions) ([]bootstrap.AuthorizedKey, error)
	AddDeploy(ctx context.Context, opts bootstrap.KeyOptions, pub []byte, apps []string, name string) (bootstrap.AuthorizedKey, error)
	Remove(ctx context.Context, opts bootstrap.KeyOptions, fingerprint string) (bootstrap.AuthorizedKey, error)
}

type realKeys struct{}

func (realKeys) List(ctx context.Context, o bootstrap.KeyOptions) ([]bootstrap.AuthorizedKey, error) {
	return bootstrap.ListKeys(ctx, o)
}

func (realKeys) AddDeploy(ctx context.Context, o bootstrap.KeyOptions, pub []byte, apps []string, name string) (bootstrap.AuthorizedKey, error) {
	return bootstrap.AddDeployKey(ctx, o, pub, apps, name)
}

func (realKeys) Remove(ctx context.Context, o bootstrap.KeyOptions, fp string) (bootstrap.AuthorizedKey, error) {
	return bootstrap.RemoveKey(ctx, o, fp)
}

func (c *cli) keyManager() keyManager {
	if c.keys != nil {
		return c.keys
	}
	return realKeys{}
}

func (c *cli) runKey(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return c.usageError("%s", keyUsage)
	}
	switch args[0] {
	case "list":
		return c.runKeyList(ctx, args[1:])
	case "add":
		return c.runKeyAdd(ctx, args[1:])
	case "remove":
		return c.runKeyRemove(ctx, args[1:])
	default:
		return c.usageError("bilinmeyen key alt komutu %q — list, add veya remove", args[0])
	}
}

// keyTarget, son argümanı root yetkili SSH hedefi olarak çözer.
func (c *cli) keyTarget(raw string, sudo bool) (bootstrap.KeyOptions, error) {
	target, err := client.ParseTarget(raw)
	if err != nil {
		return bootstrap.KeyOptions{}, err
	}
	if target.IsLocal() {
		return bootstrap.KeyOptions{}, errors.New("`key` uzak bir hedef ister (root@sunucu ya da -sudo kullanıcı@sunucu), yerel soket değil")
	}
	if c.keys == nil && !client.SSHAvailable() {
		return bootstrap.KeyOptions{}, errors.New("`ssh` komutu bulunamadı — OpenSSH istemcisi gerekli")
	}
	return bootstrap.KeyOptions{
		Host: target.SSHUser + "@" + target.SSHHost, Port: target.SSHPort, Sudo: sudo,
	}, nil
}

func (c *cli) runKeyList(ctx context.Context, args []string) int {
	fs := c.newFlagSet("key list")
	sudo := fs.Bool("sudo", false, "hedef kullanıcının PAROLASIZ sudo'suyla (parola asla sorulmaz)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 1 {
		return c.usageError("%s", keyUsage)
	}
	opts, err := c.keyTarget(fs.Arg(0), *sudo)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, keyTimeout)
	defer cancel()

	keys, err := c.keyManager().List(ctx, opts)
	if err != nil {
		return c.fail(err)
	}
	unrestricted := printKeys(c.stdout, keys)
	if unrestricted > 0 {
		return c.fail(fmt.Errorf("%d satır kadran-connect'e zorlanmamış — o anahtarlar kadran-client olarak "+
			"kabuk alabilir; kaldırın: kadran key remove <parmak-izi> %s", unrestricted, fs.Arg(0)))
	}
	return exitOK
}

// printKeys, anahtarları tablo olarak yazar ve kısıtsız satır sayısını döner.
func printKeys(w io.Writer, keys []bootstrap.AuthorizedKey) int {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "ROL\tKAPSAM\tPARMAK İZİ\tYORUM")
	unrestricted := 0
	for _, k := range keys {
		role, scope := "?", "-"
		switch {
		case !k.Restricted():
			role = "⚠ KISITSIZ"
			unrestricted++
		case k.Role == connproto.RoleAdmin:
			role = "yönetici"
		case k.Role == connproto.RoleDeploy:
			role, scope = "dağıtım", strings.Join(k.Apps, ",")
		}
		fp := k.Fingerprint
		if fp == "" {
			fp = "(okunamadı)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", role, scope, fp, printable(k.Comment))
	}
	_ = tw.Flush()
	return unrestricted
}

// printable, sunucudan gelen yorumu terminale güvenle basılır kılar:
// denetim karakterleri (terminal kaçış dizileri dahil) '?' olur.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

func (c *cli) runKeyAdd(ctx context.Context, args []string) int {
	fs := c.newFlagSet("key add")
	deploy := fs.String("deploy", "", "anahtarın dağıtabileceği uygulamalar (virgülle; ZORUNLU)")
	name := fs.String("name", "", "satırın yorumu (boşsa anahtarın kendi yorumu)")
	sudo := fs.Bool("sudo", false, "hedef kullanıcının PAROLASIZ sudo'suyla (parola asla sorulmaz)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 2 {
		return c.usageError("%s", keyUsage)
	}
	// Yönetici anahtarı bu komutla EKLENMİYOR: yönetici anahtarı
	// bootstrap'ın işi (-client-key). Kapsamsız bir `key add` yazım
	// hatasıyla tam yetki vermesin diye -deploy zorunlu.
	if *deploy == "" {
		return c.usageError("-deploy zorunlu: anahtarın dağıtabileceği uygulamaları yazın (ör. -deploy site,api)")
	}
	apps, err := connproto.ParseDeployScope(*deploy)
	if err != nil {
		return c.usageError("-deploy: %v", err)
	}
	pub, err := readPublicKey(fs.Arg(0))
	if err != nil {
		return c.fail(err)
	}
	opts, err := c.keyTarget(fs.Arg(1), *sudo)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, keyTimeout)
	defer cancel()

	k, err := c.keyManager().AddDeploy(ctx, opts, pub, apps, *name)
	if err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "Dağıtım anahtarı eklendi: %s · kapsam: %s\n\n", k.Fingerprint, strings.Join(k.Apps, ", "))
	fmt.Fprintf(c.stdout, "Bu anahtar yalnızca dağıtım yapabilir; uygulama tanımını okuyamaz, bu yüzden\n"+
		"commit açıkça verilir. GitHub Actions'ta:\n"+
		"  kadran deploy -commit \"$GITHUB_SHA\" %s %s@<sunucu>\n", apps[0], client.DefaultSSHUser)
	return exitOK
}

// readPublicKey, açık anahtar dosyasını boyut sınırıyla okur. Doğrulama
// (tek satır, özel anahtar değil, gövde türüyle uyumlu) bootstrap'ta.
func readPublicKey(path string) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // kullanıcının kendi verdiği dosya
	if err != nil {
		return nil, fmt.Errorf("açık anahtar okunamadı: %w", err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(io.LimitReader(f, maxPublicKeyFile+1))
	if err != nil {
		return nil, fmt.Errorf("açık anahtar okunamadı: %w", err)
	}
	if len(b) > maxPublicKeyFile {
		return nil, fmt.Errorf("%s açık anahtar olamayacak kadar büyük", path)
	}
	return b, nil
}

func (c *cli) runKeyRemove(ctx context.Context, args []string) int {
	fs := c.newFlagSet("key remove")
	sudo := fs.Bool("sudo", false, "hedef kullanıcının PAROLASIZ sudo'suyla (parola asla sorulmaz)")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() != 2 {
		return c.usageError("%s", keyUsage)
	}
	fp := fs.Arg(0)
	if !strings.HasPrefix(fp, "SHA256:") {
		return c.usageError("parmak izi SHA256:… biçiminde olmalı (`kadran key list` gösterir), %q değil", fp)
	}
	opts, err := c.keyTarget(fs.Arg(1), *sudo)
	if err != nil {
		return c.fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, keyTimeout)
	defer cancel()

	k, err := c.keyManager().Remove(ctx, opts, fp)
	if err != nil {
		return c.fail(err)
	}
	fmt.Fprintf(c.stdout, "Kaldırıldı: %s %s\n", k.Fingerprint, printable(k.Comment))
	return exitOK
}

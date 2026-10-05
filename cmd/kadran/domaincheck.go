package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/domaincheck"
)

// sshConfigTimeout, `ssh -G`'nin süre sınırı. Ağa çıkmıyor, yalnızca
// yapılandırmayı okuyor.
const sshConfigTimeout = 5 * time.Second

// skipDNSCheckFlag, önkontrolü atlatan bayrağın adı; hata mesajları da onu
// anıyor.
const skipDNSCheckFlag = "skip-dns-check"

func (c *cli) dnsResolver() domaincheck.Resolver {
	if c.resolver != nil {
		return c.resolver
	}
	return net.DefaultResolver
}

// resolveSSHHost, SSH hedefinin GERÇEK adını ssh yapılandırmasından okur:
// `prod` gibi bir takma adın sunucusu ancak HostName eşlemesiyle bilinir.
// Başarısız olursa adın kendisi kullanılır.
func (c *cli) resolveSSHHost(ctx context.Context, host string) string {
	lookup := c.sshHostname
	if lookup == nil {
		lookup = sshConfigHostname
	}
	if name, err := lookup(ctx, host); err == nil && name != "" {
		return name
	}
	return host
}

// sshConfigHostname, `ssh -G` çıktısındaki `hostname` satırını döndürür.
// Argv dizi olarak veriliyor ve `-` ile başlayan ad ParseTarget'ta
// reddediliyor (bkz. rejectOptionLike).
func sshConfigHostname(ctx context.Context, host string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, sshConfigTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-G", host).Output() //nolint:gosec // sabit program, kabuksuz argv
	if err != nil {
		return "", err
	}
	return parseSSHHostname(out)
}

// parseSSHHostname, `ssh -G` çıktısından `hostname` değerini alır.
func parseSSHHostname(out []byte) (string, error) {
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if name, ok := strings.CutPrefix(strings.TrimRight(sc.Text(), "\r"), "hostname "); ok {
			if name = strings.TrimSpace(name); name != "" {
				return name, nil
			}
		}
	}
	return "", errors.New("no hostname in ssh -G output")
}

// serverHostFor, hedefin sunucu adını döndürür: SSH hedefinde ssh
// yapılandırmasından çözülmüş ad, yerel hedefte boş dize. ok=false: hedef
// ayrıştırılamadı.
func (c *cli) serverHostFor(ctx context.Context, rawTarget string) (string, bool) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return "", false
	}
	if target.IsLocal() {
		return "", true
	}
	return c.resolveSSHHost(ctx, target.SSHHost), true
}

// checkDomain, `-domain` verildiğinde, sunucuya bağlanmadan ÖNCE alan
// adının DNS kaydını sunucunun adresiyle karşılaştırır (K-128). Rapor
// stderr'e gider: stdout `-json` için temiz kalmalı. Açıkça yanlış DNS'te
// hata döner; belirsiz durumda uyarıp devam eder.
func (c *cli) checkDomain(ctx context.Context, domain, rawTarget string, skip bool) error {
	if domain == "" {
		return nil
	}
	if skip {
		fmt.Fprintf(c.stderr, progName+": DNS precheck skipped (-%s)\n", skipDNSCheckFlag)
		return nil
	}
	server, ok := c.serverHostFor(ctx, rawTarget)
	if !ok {
		// Denetlenecek sunucu yok; connect aynı hatayı kendi mesajıyla
		// hemen ardından veriyor.
		return nil
	}

	rep := domaincheck.Check(ctx, c.dnsResolver(), domain, server)
	switch rep.Verdict {
	case domaincheck.OK:
		return nil
	case domaincheck.Skipped, domaincheck.Warn:
		for _, r := range rep.Reasons {
			fmt.Fprintf(c.stderr, progName+": warning: %s\n", r)
		}
		return nil
	}
	const later = "  No certificate can be issued; Caddy may wait up to a day before retrying.\n"
	if len(rep.A) == 0 && len(rep.AAAA) == 0 {
		return fmt.Errorf("%s has no DNS record\n"+later+
			"  Add an A record pointing the domain at the server's address and try again\n"+
			"  (if you just added it, wait for it to propagate or skip with -%s)",
			rep.Domain, skipDNSCheckFlag)
	}
	return fmt.Errorf("%s does not point at this server: %s\n"+later+
		"  Fix the DNS and try again. If the domain is behind a proxy (e.g. Cloudflare)\n"+
		"  this is expected: skip with -%s",
		rep.Domain, strings.Join(rep.Reasons, "; "), skipDNSCheckFlag)
}

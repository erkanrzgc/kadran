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

	"github.com/erkanrzgc/panely/internal/client"
	"github.com/erkanrzgc/panely/internal/domaincheck"
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
	return "", errors.New("ssh -G çıktısında hostname yok")
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
		fmt.Fprintf(c.stderr, "panely: DNS önkontrolü atlandı (-%s)\n", skipDNSCheckFlag)
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
			fmt.Fprintf(c.stderr, "panely: uyarı: %s\n", r)
		}
		return nil
	}
	const later = "  Sertifika alınamaz; Caddy yeniden denemeden önce 1 güne kadar bekleyebilir.\n"
	if len(rep.A) == 0 && len(rep.AAAA) == 0 {
		return fmt.Errorf("%s için DNS kaydı yok\n"+later+
			"  Alan adına sunucunun adresini gösteren bir A kaydı ekleyip yeniden deneyin\n"+
			"  (kayıt yeni eklendiyse yayılmasını bekleyin ya da -%s ile geçin)",
			rep.Domain, skipDNSCheckFlag)
	}
	return fmt.Errorf("%s bu sunucuyu göstermiyor: %s\n"+later+
		"  DNS'i düzeltip yeniden deneyin. Alan adı bir vekilin (ör. Cloudflare)\n"+
		"  arkasındaysa bu beklenir: -%s ile geçin",
		rep.Domain, strings.Join(rep.Reasons, "; "), skipDNSCheckFlag)
}

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
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

// dnsChecker, önkontrol ve `domain check` için denetleyici (K-151). Gerçek
// çözümleyicide NS ve yetkili sorgular açık. Testte (resolver sahte) NS
// yalnız test kendi sahtesini verirse açılır, yetkili sorgu da yalnız test
// sahtesiyle: CI gerçek DNS'e çıkmasın.
func (c *cli) dnsChecker() domaincheck.Checker {
	if c.resolver == nil {
		return domaincheck.Checker{Resolver: net.DefaultResolver, NS: net.DefaultResolver}
	}
	ch := domaincheck.Checker{Resolver: c.resolver, Exchange: c.exchange}
	if c.nsResolver != nil {
		ch.NS = c.nsResolver
	}
	if ch.Exchange == nil {
		ch.Exchange = offlineExchange
	}
	return ch
}

// offlineExchange, testte yetkili sorguyu kapatır.
func offlineExchange(context.Context, netip.AddrPort, []byte) ([]byte, error) {
	return nil, errors.New("authoritative DNS queries are off in this test")
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

// waitHint, Stop raporunun "düzelttikten sonra" satırı. Alan adı ve hedef
// kullanıcı girdisi; hata metni kopyalanıp kabuğa yapıştırılır, bu yüzden
// yalnız düz değerler basılır, aksi hâlde satır hiç basılmaz (C2).
func waitHint(domain, rawTarget string) string {
	if !domaincheck.PlainName(domain) || !plainTarget(rawTarget) {
		return ""
	}
	cmd := "kadran domain check -wait 10m " + domain
	if rawTarget != "" {
		cmd += " " + rawTarget
	}
	return "  After fixing it: " + cmd + "\n"
}

// plainTarget: hedef boş ya da [A-Za-z0-9._@:-] ve "-" ile başlamıyor.
func plainTarget(s string) bool {
	if strings.HasPrefix(s, "-") {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("._@:-", r) {
			return false
		}
	}
	return true
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

	rep := c.dnsChecker().Check(ctx, domain, server)
	switch rep.Verdict {
	case domaincheck.OK:
		return nil
	case domaincheck.Skipped, domaincheck.Warn:
		for _, r := range rep.Reasons {
			fmt.Fprintf(c.stderr, progName+": warning: %s\n", r)
		}
		return nil
	}
	var b strings.Builder
	if len(rep.A) == 0 && len(rep.AAAA) == 0 && !rep.Auth.Answered {
		fmt.Fprintf(&b, "%s has no DNS record\n", rep.Domain)
	} else {
		fmt.Fprintf(&b, "%s does not point at this server: %s\n", rep.Domain, strings.Join(rep.Reasons, "; "))
	}
	writeDNSGuide(&b, rep)
	b.WriteString("  No certificate can be issued; Caddy may wait up to a day before retrying.\n")
	b.WriteString(waitHint(rep.Domain, rawTarget))
	fmt.Fprintf(&b, "  If the domain is behind a proxy on purpose, skip with -%s", skipDNSCheckFlag)
	return errors.New(b.String())
}

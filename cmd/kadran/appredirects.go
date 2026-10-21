package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/erkanrzgc/kadran/internal/appnames"
	"github.com/erkanrzgc/kadran/internal/domaincheck"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// redirectsLine, uygulamanın www eşini ve açık yönlendirme adlarını tek
// satırda gösterir; yoksa boş dize.
func redirectsLine(spec *kadranv1.AppSpec) string {
	names := appnames.Names(spec.GetDomain(), spec.GetWww(), spec.GetRedirects())
	if len(names) == 0 {
		return ""
	}
	return fmt.Sprintf("  Redirects: %s → %s (308)\n", strings.Join(names, ", "), spec.GetDomain())
}

// serverKnowsRedirects: sunucu cevabında Www HER ZAMAN doludur; eski bir
// sunucu alanı hiç göndermez.
func serverKnowsRedirects(spec *kadranv1.AppSpec) bool { return spec.Www != nil }

// afterRedirectReply, sunucunun cevabından sonra çağrılır: istek www ya da
// yönlendirme taşıyıp sunucu bunu bilmiyorsa uyarır, biliyorsa adların DNS'ini
// engellemeden denetler. Redirects satırı basılıp basılmayacağını döndürür.
func (c *cli) afterRedirectReply(ctx context.Context, spec *kadranv1.AppSpec, asked, check bool, rawTarget string, skip bool) bool {
	if !serverKnowsRedirects(spec) {
		if asked {
			fmt.Fprintln(c.stderr, progName+": warning: this server does not support redirects (www, -redirect); upgrade kadrand")
		}
		return false
	}
	if check {
		c.checkRedirectNames(ctx, appnames.Names(spec.GetDomain(), spec.GetWww(), spec.GetRedirects()), rawTarget, skip)
	}
	return true
}

// checkRedirectNames, yönlendirme adlarının DNS'ini PARALEL denetler. Çıkış
// kodunu ASLA değiştirmez: yanlış kayıt yalnız uyarıdır, ad DNS düzelince
// kendiliğinden çalışır.
func (c *cli) checkRedirectNames(ctx context.Context, names []string, rawTarget string, skip bool) {
	if skip || len(names) == 0 {
		return
	}
	server, ok := c.serverHostFor(ctx, rawTarget)
	if !ok {
		return
	}
	checker := c.dnsChecker()
	reports := make([]domaincheck.Report, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports[i] = checker.Check(ctx, name, server)
		}()
	}
	wg.Wait()

	// Yazım tek goroutine'de ve ad sırasıyla: çıktı kararlı kalır.
	for i, rep := range reports {
		if rep.Verdict != domaincheck.Stop && rep.Verdict != domaincheck.Warn {
			continue
		}
		fmt.Fprintf(c.stderr, progName+": warning: %s does not point at the server yet; it is redirected once it does and gets its own certificate\n", names[i])
		writeDNSGuide(c.stderr, rep)
	}
}

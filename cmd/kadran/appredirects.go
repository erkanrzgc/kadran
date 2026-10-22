package main

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

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

// redirectsUnsupported, eski (yönlendirmeyi bilmeyen) sunucu için ipucu.
const redirectsUnsupported = "this server does not support redirects (www, -redirect); upgrade kadrand"

// serverKnowsRedirects: sunucu cevabında Www HER ZAMAN doludur; eski bir
// sunucu alanı hiç göndermez.
func serverKnowsRedirects(spec *kadranv1.AppSpec) bool { return spec.Www != nil }

// afterRedirectReply, sunucunun cevabından sonra çağrılır: istek www ya da
// yönlendirme taşıyıp sunucu bunu bilmiyorsa uyarır, biliyorsa adların DNS'ini
// engellemeden denetler. Redirects satırı basılıp basılmayacağını döndürür.
func (c *cli) afterRedirectReply(ctx context.Context, spec *kadranv1.AppSpec, asked, check bool, rawTarget string, skip bool) bool {
	if !serverKnowsRedirects(spec) {
		if asked {
			fmt.Fprintln(c.stderr, progName+": warning: "+redirectsUnsupported)
		}
		return false
	}
	if check {
		c.checkRedirectNames(ctx, appnames.Names(spec.GetDomain(), spec.GetWww(), spec.GetRedirects()), rawTarget, skip)
	}
	return true
}

// oldServerRejectedNames, yalnız ad alanı (-www/-no-www/-redirect/-redirect-rm)
// taşıyan bir güncellemenin ESKİ sunucuda reddedildiğini tanır: alanları
// bilmeyen sunucu isteği boş sayar ve "no field given" ile InvalidArgument
// döner. Yeni sunucunun doğrulama hataları (dokuzuncu ad, geçersiz ad) da
// InvalidArgument; onlar METİNLERİYLE ayrılır ve olduğu gibi basılır.
func oldServerRejectedNames(req *kadranv1.UpdateAppRequest, err error) bool {
	if req.Www == nil && len(req.GetRedirectAdd()) == 0 && len(req.GetRedirectRemove()) == 0 {
		return false
	}
	rest, ok := proto.Clone(req).(*kadranv1.UpdateAppRequest)
	if !ok {
		return false
	}
	rest.Www, rest.RedirectAdd, rest.RedirectRemove = nil, nil, nil
	if !isEmptyUpdate(rest) {
		return false
	}
	st, ok := status.FromError(err)
	return ok && st.Code() == codes.InvalidArgument && strings.Contains(st.Message(), "no field given")
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
		switch rep.Verdict {
		case domaincheck.Stop:
			fmt.Fprintf(c.stderr, progName+": warning: %s does not point at the server yet; it is redirected once it does and gets its own certificate\n", names[i])
			writeDNSGuide(c.stderr, rep)
		case domaincheck.Warn:
			// Warn "göstermiyor" demek değil: DNS firması sunucuyu zaten
			// cevaplıyor ve yalnız bu makinenin önbelleği eski olabilir.
			// `domain check`'in gerekçeleri olduğu gibi basılır; gerekçe adı
			// anmadığı için ad başa yazılır.
			for _, r := range rep.Reasons {
				fmt.Fprintf(c.stderr, progName+": warning: %s: %s\n", names[i], r)
			}
			fmt.Fprintf(c.stderr, "  %s is redirected and gets its own certificate once it resolves to the server; old answers cached elsewhere expire on their own\n", names[i])
		}
	}
}

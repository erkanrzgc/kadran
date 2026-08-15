package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// appUpdateFlags, `app update`'in kabul ettiği seçeneklerin değerleridir.
type appUpdateFlags struct {
	domain   string
	branch   string
	health   string
	replicas uint
}

// runAppUpdate, var olan bir uygulamanın alanlarını değiştirir.
func (c *cli) runAppUpdate(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app update")
	var v appUpdateFlags
	fs.StringVar(&v.domain, "domain", "",
		"yayınlanacak alan adı; boş verilirse (-domain=\"\") uygulama vekilden ÇIKARILIR")
	fs.StringVar(&v.branch, "branch", "", "varsayılan dal")
	fs.StringVar(&v.health, "health-path", "",
		"sağlık yoklaması yolu; boş verilirse yoklama YAPILMAZ")
	fs.UintVar(&v.replicas, "replicas", 0, "replika sayısı")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", defaultTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely app update [seçenekler] <ad> [hedef] — " +
			"seçenekler addan ÖNCE gelir")
	}

	// ⚠ VERİLEN seçenekler, DEĞERLERİ değil.
	//
	// Go'nun flag paketi "verilmedi" ile "sıfır değeriyle verildi"yi aynı
	// şeye indirger: `-domain=""` ile `-domain` hiç yazılmamış olması
	// ikisi de boş dize üretir. O ayrımı kaybetmek, alan adına dokunmak
	// istemeyen her komutun onu SESSİZCE silmesi demekti.
	//
	// fs.Visit YALNIZCA gerçekten ayarlanmış seçenekleri gezer. Bu,
	// şemadaki `optional` kararının komut satırı tarafındaki karşılığı:
	// ayrım baştan sona korunuyor.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	req := buildUpdateRequest(fs.Arg(0), v, set)
	if isEmptyUpdate(req) {
		return c.usageError("değiştirilecek bir alan verilmedi — " +
			"-domain, -branch, -health-path veya -replicas kullanın")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().UpdateApp(ctx, req)
	if err != nil {
		return c.fail(fmt.Errorf("uygulama güncellenemedi: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	s := resp.GetApp().GetSpec()
	fmt.Fprintf(c.stdout, "Uygulama güncellendi: %s\n", s.GetAppId())
	if req.Domain != nil {
		fmt.Fprintf(c.stdout, "  Alan adı: %s\n", orNone(s.GetDomain()))
	}
	if req.GitBranch != nil {
		fmt.Fprintf(c.stdout, "  Dal     : %s (bir sonraki dağıtımda kullanılır)\n", s.GetGitBranch())
	}
	if req.HealthPath != nil {
		fmt.Fprintf(c.stdout, "  Sağlık  : %s (bir sonraki dağıtımda etkili)\n", orNone(s.GetHealthPath()))
	}
	if req.Replicas != nil {
		fmt.Fprintf(c.stdout, "  Replika : %d (bir sonraki dağıtımda etkili)\n", s.GetReplicas())
	}

	// Ters vekilin durumu SUSULAMAZ. Alan adı değişip trafiğin
	// taşınmaması mümkün ve o durumda "güncellendi" tek başına yanıltıcı.
	if d := resp.GetProxyDetail(); d != "" {
		fmt.Fprintf(c.stdout, "\n%s\n", d)
	}
	return exitOK
}

// buildUpdateRequest, YALNIZCA komut satırında verilmiş alanları isteğe
// koyar.
//
// Ayrı bir fonksiyon olması testin gereği değil, testin MÜMKÜN olmasının
// şartı: `set` haritasını doğrudan vermek, alan adının BOŞ VERİLMESİ ile
// HİÇ VERİLMEMESİ durumlarını bir FlagSet kurmadan yan yana sınamayı
// sağlıyor.
func buildUpdateRequest(appID string, v appUpdateFlags, set map[string]bool) *panelyv1.UpdateAppRequest {
	req := &panelyv1.UpdateAppRequest{AppId: appID}
	if set["domain"] {
		req.Domain = &v.domain
	}
	if set["branch"] {
		req.GitBranch = &v.branch
	}
	if set["health-path"] {
		req.HealthPath = &v.health
	}
	if set["replicas"] {
		r := uint32(v.replicas) //nolint:gosec // sunucu 1-64 doğruluyor
		req.Replicas = &r
	}
	return req
}

func isEmptyUpdate(req *panelyv1.UpdateAppRequest) bool {
	return req.Domain == nil && req.GitBranch == nil &&
		req.HealthPath == nil && req.Replicas == nil
}

// orNone, boş değeri görünür kılar.
//
// Boş bir satır ("Alan adı: ") kullanıcıya değerin ne olduğunu değil,
// çıktının bozuk olduğunu düşündürür.
func orNone(s string) string {
	if s == "" {
		return "(yok)"
	}
	return s
}

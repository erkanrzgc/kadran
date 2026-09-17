package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/erkanrzgc/panely/internal/client"
	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runPrune, eski sürümlerin konteynerlerini kaldırır.
//
// ── `-all` neden AÇIK bir bayrak ────────────────────────────────────
//
// Sunucu boş `app_id`'yi reddediyor: proto3'te string presence
// taşımadığı için "gönderilmedi" ile "boş gönderildi" telde ayırt
// edilemez ve boşu "hepsi" saymak, alanı doldurmayı unutan bir çağıranın
// YIKICI bir işlemi her uygulamaya uygulatması demekti.
//
// "Hepsini buda" isteği bu yüzden burada, ayrıcalıksız kodda, AÇIK bir
// bayrak ve bir döngü olarak kuruluyor. Buradaki bir hata geri
// alınabilir; şemadaki bir belirsizlik değil.
//
// ⚠ Bayraklar uygulama adından ÖNCE gelir (Go'nun `flag` paketi ilk
// positional'da durur): `panely prune -dry-run pfprobe`.
func (c *cli) runPrune(ctx context.Context, args []string) int {
	fs := c.newFlagSet("prune")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	dry := fs.Bool("dry-run", false, "ne silineceğini yazar, SİLMEZ")
	all := fs.Bool("all", false, "bütün uygulamaları budar")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	// Hedef argümanının yeri `-all` ile değişiyor; ikisi birden
	// verilirse hangi positional'ın ne olduğu belirsizleşirdi.
	var appID, target string
	switch {
	case *all && fs.NArg() > 1:
		return c.usageError("kullanım: panely prune -all [hedef]")
	case *all:
		target = fs.Arg(0)
	case fs.NArg() < 1 || fs.NArg() > 2:
		return c.usageError("kullanım: panely prune <uygulama> [hedef] " +
			"ya da panely prune -all [hedef] — seçenekler uygulama adından ÖNCE gelir")
	default:
		appID, target = fs.Arg(0), fs.Arg(1)
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx, target)
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	ids := []string{appID}
	if *all {
		list, err := conn.RPC().ListApps(ctx, &panelyv1.ListAppsRequest{})
		if err != nil {
			return c.fail(fmt.Errorf("prune -all: %w", err))
		}
		ids = ids[:0]
		for _, a := range list.GetApps() {
			ids = append(ids, a.GetSpec().GetAppId())
		}
		if len(ids) == 0 {
			fmt.Fprintln(c.stdout, "budanacak uygulama yok")
			return exitOK
		}
	}

	return c.pruneEach(ctx, conn, ids, *dry, *asJSON, *all)
}

// pruneEach, verilen uygulamaları tek tek budar.
//
// ── Bir uygulamanın hatası ötekileri DURDURMAZ ──────────────────────
//
// `-all` ile koşarken hiç dağıtılmamış bir uygulama FailedPrecondition
// döndürüyor (sunucu fail-closed). İlk hatada durmak, alfabetik olarak
// sonra gelen sağlam uygulamaların hiç budanmaması demekti — üstelik
// sebebi kullanıcının umursamadığı bir uygulama.
//
// Hatalar yutulmuyor: adıyla yazılıyor ve çıkış kodu hata veriyor.
func (c *cli) pruneEach(
	ctx context.Context, conn *client.Client, ids []string, dry, asJSON, all bool,
) int {
	var (
		results []json.RawMessage
		failed  int
	)
	for _, id := range ids {
		resp, err := conn.RPC().PruneApp(ctx, &panelyv1.PruneAppRequest{
			AppId: id, DryRun: dry,
		})
		if err != nil {
			if !all {
				return c.fail(fmt.Errorf("prune: %w", err))
			}
			fmt.Fprintf(c.stderr, "%s: atlandı — %v\n", id, err)
			failed++
			continue
		}
		if asJSON {
			body, err := protoToJSON(resp)
			if err != nil {
				return c.fail(err)
			}
			results = append(results, json.RawMessage(body))
			continue
		}
		c.printPrune(resp, dry)
	}

	if asJSON {
		return c.writeJSON(results)
	}
	if failed > 0 {
		return exitError
	}
	return exitOK
}

// printPrune, tek bir budamanın sonucunu yazar.
//
// ⚠ NE SİLİNDİĞİ kadar NE SİLİNMEDİĞİ de yazılıyor (K-088). "3
// konteyner silindi" operatöre en çok merak ettiği şeyi söylemiyor:
// geri alma hâlâ çalışıyor mu, ve disk gerçekten toparlandı mı?
func (c *cli) printPrune(resp *panelyv1.PruneAppResponse, dry bool) {
	head := "budandı"
	if dry {
		head = "DENEME (hiçbir şey silinmedi)"
	}

	pruned := resp.GetPrunedReleases()
	if len(pruned) == 0 {
		fmt.Fprintf(c.stdout, "%s: budanacak eski sürüm yok · korunan: %s\n",
			resp.GetAppId(), strings.Join(resp.GetKeptReleases(), ", "))
		return
	}

	fmt.Fprintf(c.stdout, "%s %s · %d konteyner · sürümler: %s\n",
		resp.GetAppId(), head, resp.GetContainersRemoved(),
		strings.Join(pruned, ", "))

	// Korunanlar SEBEBİYLE yazılıyor: "r5 korundu" ile "r5 (geri alma
	// hedefi)" arasındaki fark, operatörün rollback'in hâlâ hızlı
	// olduğunu bilmesi.
	fmt.Fprintf(c.stdout, "  korunan: %s\n",
		strings.Join(resp.GetKeptReleases(), ", "))

	if resp.GetImagesUntouched() {
		fmt.Fprintf(c.stdout,
			"  ⚠ İMAJLARA DOKUNULMADI — `panely/%s:<sha>` imajları yerinde.\n"+
				"    Silinmiş uygulamaların yetim konteynerleri de kapsam dışı.\n",
			resp.GetAppId())
	}
}

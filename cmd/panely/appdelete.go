package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// runAppDelete, uygulamayı ve izlerini kaldırır.
//
// ── Onay istemiyor, ve bu kasıtlı ───────────────────────────────────
//
// Bir CLI çağrısı zaten açık bir niyettir; üstüne "emin misiniz?"
// sormak, operatörü refleksle `y` yazmaya alıştırmaktan başka bir şey
// yapmaz. Gerçek koruma başka yerde ve daha sert: canlı sürümü olan bir
// uygulama sunucu tarafından REDDEDİLİYOR, yani bu komutla çalışan bir
// siteyi düşürmek imkânsız.
//
// ⚠ Hata ön eki yok (K-068): sunucunun hataları kendi kendini
// açıklıyor ("uygulamanın canlı sürümü var (r1) — ...") ve sonuç iddia
// eden bir ön ek, kullanıcıyı ilk üç kelimeden yanlış sonuca götürür.
func (c *cli) runAppDelete(ctx context.Context, args []string) int {
	fs := c.newFlagSet("app delete")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely app delete <uygulama> [hedef] — " +
			"seçenekler uygulama adından ÖNCE gelir")
	}

	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().DeleteApp(ctx, &panelyv1.DeleteAppRequest{AppId: fs.Arg(0)})
	if err != nil {
		return c.fail(fmt.Errorf("app delete: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}
	// NE yok edildiği yazdırılıyor: "silindi" tek başına hiçbir şey
	// söylemiyor, çünkü boş bir kayıt da beş sürümlü bir uygulama da
	// aynı cevabı verirdi.
	fmt.Fprintf(c.stdout, "%s silindi · %d konteyner · %d sürüm · %d dağıtım kaydı\n",
		resp.GetAppId(), resp.GetContainersRemoved(),
		resp.GetReleasesDeleted(), resp.GetDeploymentsDeleted())

	// ⚠ NE SİLİNMEDİĞİ de söylenmeli.
	//
	// `app delete` hacim verisine dokunmuyor — geri alınamaz bir veri
	// kaybının tek bir komutun yan etkisi olması bu projede kabul
	// edilmiyor. Ama sessiz kalmak da kabul edilemez: kullanıcı
	// uygulamayı sildiğinde diskin de gittiğini varsayar ve aylarca
	// farkına varmadan yer tüketir.
	//
	// Mesaj yolu ADLANDIRMIYOR: hacim kökü executor'ın yapılandırması ve
	// panelyd onu bilmiyor. Bilmediği bir yolu yazmak, doğrulanmamış bir
	// iddia olurdu.
	if kept := resp.GetVolumesKept(); len(kept) > 0 {
		fmt.Fprintf(c.stdout,
			"\n⚠ %d hacmin VERİSİ DİSKTE DURUYOR (silinmedi): %s\n"+
				"  Panely kalıcı diski kendiliğinden yok etmez. Yer açmak "+
				"isterseniz sunucudaki hacim dizinini elle kaldırın.\n",
			len(kept), strings.Join(kept, ", "))
	}
	return exitOK
}

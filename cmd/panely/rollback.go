package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	panelyv1 "github.com/erkanrzgc/panely/internal/pb/panely/v1"
)

// rollbackTimeout, geri almanın varsayılan süre sınırı.
//
// ── Neden defaultTimeout (30 sn) DEĞİL? ─────────────────────────────
//
// Geri alma hiçbir şey derlemez ama sağlık kapısından geçer ve kapının
// kendi sınırı 90 saniye (deploy.DefaultGate). 30 saniyelik bir istemci
// sınırı, kapı daha karar vermeden bağlantıyı koparırdı: sunucu geri
// almayı tamamlar, kullanıcı "başarısız" görürdü ve muhtemelen ikinci kez
// çalıştırırdı. Bu, K-044'ün (60 sn'lik istemci sınırı 75 sn'lik derlemeyi
// öldürüyordu) aynı sınıfı.
//
// Kapının üstüne konteyner başlatma ve ters vekil yüklemesi için pay
// bırakılıyor.
const rollbackTimeout = 3 * time.Minute

// runRollback, trafiği bir önceki sürüme geri çevirir.
//
// Hiçbir şey derlenmez: hedef sürümün imajı zaten var ve konteynerleri
// çoğu zaman durmuş hâlde bekliyor, yani işlem saniyeler sürer.
func (c *cli) runRollback(ctx context.Context, args []string) int {
	fs := c.newFlagSet("rollback")
	asJSON := fs.Bool("json", false, "makine okunabilir JSON çıktısı")
	timeout := fs.Duration("timeout", rollbackTimeout, "toplam süre sınırı")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}
	if fs.NArg() < 1 || fs.NArg() > 2 {
		return c.usageError("kullanım: panely rollback <uygulama> [hedef] — " +
			"seçenekler uygulama adından ÖNCE gelir")
	}

	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()

	conn, _, err := c.connect(ctx, fs.Arg(1))
	if err != nil {
		return c.fail(err)
	}
	defer func() { _ = conn.Close() }()

	resp, err := conn.RPC().Rollback(ctx, &panelyv1.RollbackRequest{AppId: fs.Arg(0)})
	if err != nil {
		// ⚠ "geri alınamadı" ÖN EKİ YOK ve bu K-068'in dersi. Sunucunun
		// hataları kendi kendini açıklıyor ("geri alınacak önceki sürüm
		// yok", "sağlık kapısında durdu, TRAFİK TAŞINMADI") ve bir ön ek
		// sonuç iddia ederse kullanıcı ilk üç kelimeyi okuyup yanlış
		// sonuca varır. Ön ek yalnızca hangi komutun konuştuğunu söylüyor.
		return c.fail(fmt.Errorf("rollback: %w", err))
	}

	if *asJSON {
		body, err := protoToJSON(resp)
		if err != nil {
			return c.fail(err)
		}
		return c.writeJSON(json.RawMessage(body))
	}

	fmt.Fprintf(c.stdout, "Geri alındı: %s\n", resp.GetAppId())
	fmt.Fprintf(c.stdout, "  %s → %s\n", resp.GetFromReleaseId(), resp.GetToReleaseId())

	// Konteynerlerin yeniden kurulup kurulmadığı SUSULMUYOR: operatörün
	// "neden saniyeler değil de yarım dakika sürdü" sorusunu günlüğe
	// bakmadan yanıtlaması gerekiyor.
	if resp.GetRecreated() {
		fmt.Fprintf(c.stdout,
			"  Not: konteynerler hostta yoktu, imajdan yeniden kuruldu.\n")
	}
	return exitOK
}

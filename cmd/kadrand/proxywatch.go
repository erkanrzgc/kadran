package main

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/erkanrzgc/kadran/internal/alarm"
	"github.com/erkanrzgc/kadran/internal/deploy"
	"github.com/erkanrzgc/kadran/internal/liveness"
	"github.com/erkanrzgc/kadran/internal/store"
)

// ── Vekil izleyicisi: K-055'in ikinci yarısı ─────────────────────────
//
// Ters vekil `--resume` kullanmıyor (K-055, bilinçli): yeniden başlayınca
// rotasız açılıyor ve gerçeğin kaynağı SQLite. Karar bir yükümlülük
// doğurmuştu — kadrand "açılışta VE ters vekil yeniden başladığında"
// uzlaştırmak zorunda. İlk yarı yapılmıştı, ikinci yarı HİÇ.
//
// Taze sunucu testinde (K-112) ölçüldü: yalnızca kadran-caddy yeniden
// başlatıldı ve site 40 saniyenin 40'ında da kapalı kaldı; kadrand tek
// satır yazmadı. Yani Caddy'nin bir çökmesi, systemd onu geri getirse
// bile, bütün siteleri kadrand yeniden başlayana kadar kapalı bırakırdı —
// ve K-110'un bildirimi "systemd yeniden başlattı" diyerek rahatlatırdı.
//
// `--resume` (K-055'i tersine çevirir) ve birimler arası PartOf
// (Caddy'nin bir çökme döngüsü kadrand'yi de döndürürdü) reddedildi.

// proxyWatchInterval, iki denetim arasındaki süre: bir Caddy yeniden
// başlatmasından sonra en uzun kesinti ≈ bu süre + yükleme.
const proxyWatchInterval = 10 * time.Second

// proxyWatchAlarmAfter: üst üste bu kadar başarısız denetim alarm. Tek
// başarısızlık değil: Caddy yeniden başlarken admin soketi saniyeler
// içinde cevap vermeyebilir.
const proxyWatchAlarmAfter = 3

// proxyAlarmID, açılış uzlaştırmasının da kullandığı alarm.
const proxyAlarmID = alarm.KindProxyUnreconciled + ":host"

type proxyRepairer interface {
	Repair(ctx context.Context) (deploy.RepairResult, error)
}

type alarmRaiser interface {
	Raise(ctx context.Context, a store.Alarm)
	Clear(ctx context.Context, id string)
}

type proxyWatcher struct {
	rp       proxyRepairer
	am       alarmRaiser
	failures int
	// skippedRedirects, en son günlüğe yazılan uygulanamayan yönlendirme
	// adları. Tur on saniyede bir; aynı kümeyi her turda yazmak günlüğü
	// boğardı, bu yüzden yalnız küme DEĞİŞİNCE yazılır.
	skippedRedirects map[string]string
}

// tick, bir denetim turu.
//
// ── Alarm ne zaman kapanır ──────────────────────────────────────────
//
// Alarmın dediği şey ("trafik akmıyor") artık doğru DEĞİLSE: rotalanamayan
// (atlanan) uygulama yok VE canlı yapılandırma beklenenle İKİ YÖNLÜ
// birebir. Alarmı kimin açtığı önemli değil; Clear etkin alarm yoksa
// sessiz (alarm.Manager), her turda çağrılabilir.
//
// İki önceki hâl de yanlıştı:
//   - Onarımdan sonra koşulsuz kapatmak, açılıştaki "rotalanamayan
//     uygulama var" alarmını gizliyordu (güvenlik incelemesi).
//   - Yalnızca KENDİ açtığını ya da kendi onardığını kapatmak, reboot'tan
//     sonra alarmı SONSUZA DEK açık bıraktı: gözetmen uygulamayı
//     iyileştirip rotayı yüklüyor, izleyici "ben onarmadım" diye
//     dokunmuyordu (taze sunucuda ölçüldü, K-112).
//
// "Eksik yok, atlanan yok" YETMEZ: açılış alarmı canlıda gönderilmemiş
// bir rota yüzünden de açılıyor (K-054) ve o hâlde eksik rota yoktur.
func (w *proxyWatcher) tick(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, startupReconcileTimeout)
	sonuc, err := w.rp.Repair(c)
	cancel()
	missing := sonuc.Missing

	if err != nil {
		w.failures++
		slog.Warn("reverse proxy check failed", "consecutive", w.failures, "error", err)
		if w.failures >= proxyWatchAlarmAfter {
			w.am.Raise(ctx, store.Alarm{
				ID:       proxyAlarmID,
				Kind:     alarm.KindProxyUnreconciled,
				Target:   "host",
				Severity: store.SeverityCritical,
				Since:    time.Now(),
				Detail: fmt.Sprintf("reverse proxy has failed to reconcile for %d consecutive checks, "+
					"TRAFFIC MAY NOT BE FLOWING: %v", w.failures, err),
			})
		}
		return
	}

	w.failures = 0
	if len(missing) > 0 {
		slog.Warn("reverse proxy had lost its routes — reloaded", "missing", missing)
	}
	// Atlanan ad alarmı ETKİLEMEZ (domain rotaları yerinde); yalnız günlük.
	if !maps.Equal(sonuc.SkippedRedirects, w.skippedRedirects) {
		deploy.WarnSkippedRedirects(sonuc.SkippedRedirects)
		w.skippedRedirects = maps.Clone(sonuc.SkippedRedirects)
	}
	if sonuc.Exact && len(sonuc.Skipped) == 0 {
		w.am.Clear(ctx, proxyAlarmID)
	}
}

// watchProxy, izleyiciyi kapanışa kadar koşturur. Her turdan sonra
// watchdog'un damgasını tazeler (K-115).
func watchProxy(ctx context.Context, w *proxyWatcher, every time.Duration, beat *liveness.Beat) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
			beat.Mark()
		}
	}
}

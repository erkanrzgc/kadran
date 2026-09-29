package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/erkanrzgc/panely/internal/alarm"
	"github.com/erkanrzgc/panely/internal/deploy"
	"github.com/erkanrzgc/panely/internal/liveness"
	"github.com/erkanrzgc/panely/internal/store"
)

// ── Vekil izleyicisi: K-055'in ikinci yarısı ─────────────────────────
//
// Ters vekil `--resume` kullanmıyor (K-055, bilinçli): yeniden başlayınca
// rotasız açılıyor ve gerçeğin kaynağı SQLite. Karar bir yükümlülük
// doğurmuştu — panelyd "açılışta VE ters vekil yeniden başladığında"
// uzlaştırmak zorunda. İlk yarı yapılmıştı, ikinci yarı HİÇ.
//
// Taze sunucu testinde (K-112) ölçüldü: yalnızca panely-caddy yeniden
// başlatıldı ve site 40 saniyenin 40'ında da kapalı kaldı; panelyd tek
// satır yazmadı. Yani Caddy'nin bir çökmesi, systemd onu geri getirse
// bile, bütün siteleri panelyd yeniden başlayana kadar kapalı bırakırdı —
// ve K-110'un bildirimi "systemd yeniden başlattı" diyerek rahatlatırdı.
//
// `--resume` (K-055'i tersine çevirir) ve birimler arası PartOf
// (Caddy'nin bir çökme döngüsü panelyd'yi de döndürürdü) reddedildi.

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
		slog.Warn("ters vekil denetlenemedi", "ust_uste", w.failures, "hata", err)
		if w.failures >= proxyWatchAlarmAfter {
			w.am.Raise(ctx, store.Alarm{
				ID:       proxyAlarmID,
				Kind:     alarm.KindProxyUnreconciled,
				Target:   "host",
				Severity: store.SeverityCritical,
				Since:    time.Now(),
				Detail: fmt.Sprintf("ters vekil %d denetimdir üst üste uzlaştırılamıyor, "+
					"TRAFİK AKMIYOR OLABİLİR: %v", w.failures, err),
			})
		}
		return
	}

	w.failures = 0
	if len(missing) > 0 {
		slog.Warn("ters vekil rotalarını kaybetmişti — yeniden yüklendi", "eksik", missing)
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

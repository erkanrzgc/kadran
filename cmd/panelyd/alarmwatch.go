package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/erkanrzgc/panely/internal/alarm"
	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/store"
)

// Disk eşikleri: boş alanın toplama oranı.
//
// ── Neden iki eşik ve neden GERİ DÖNÜŞ eşiği farklı ─────────────────
//
// Tek eşik olsaydı, tam sınırın etrafında gidip gelen bir disk her
// turda alarm açıp kapatırdı — bu projenin ölümcül saydığı gürültünün
// ta kendisi. Kapanma eşiği açılma eşiğinden YÜKSEK (histerezis):
// alarm %15'te açılır, ancak %20'nin üstüne çıkınca kapanır.
//
// Kritik eşik %5: bu noktada dolu disk hem yeni dağıtımı hem SQLite
// yazmalarını durdurur (common.proto'daki gerekçe), yani artık
// "bilmen iyi olur" değil "şimdi bak".
const (
	diskWarnFree     = 0.15
	diskCriticalFree = 0.05
	diskClearFree    = 0.20
)

// diskAlarmID, disk alarmının sabit kimliği. Hedef tek bir dosya
// sistemi olduğu için uygulama başına ayrışmıyor.
const diskAlarmID = alarm.KindDiskLow + ":host"

// watchDisk, düzenli aralıklarla disk doluluğunu ölçer ve alarmı
// günceller.
//
// ── Neden gözetmenin içinde değil ───────────────────────────────────
//
// Gözetmen UYGULAMALARI izliyor ve turu 2 saniye. Disk ölçümü
// executor'a bir RPC demek; iki saniyede bir sormak, ölçtüğü şeyin
// değişim hızıyla tamamen orantısız olurdu. Disk dakikalar içinde
// dolar, saniyeler içinde değil.
func watchDisk(
	ctx context.Context, exec *execclient.Client, am *alarm.Manager,
	every time.Duration,
) {
	if every <= 0 {
		slog.Warn("disk alarmı KAPALI", "sebep", "--disk-check-interval 0")
		return
	}

	checkDisk(ctx, exec, am)

	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkDisk(ctx, exec, am)
		}
	}
}

// checkDisk, tek bir ölçüm yapar.
func checkDisk(ctx context.Context, exec *execclient.Client, am *alarm.Manager) {
	probe, cancel := context.WithTimeout(ctx, execclient.DefaultTimeout)
	defer cancel()

	info, err := exec.HostInfo(probe)
	if err != nil {
		// ⚠ Ölçemiyorsak alarm AÇMIYORUZ.
		//
		// "Executor'a ulaşamadım" ile "disk doldu" aynı şey değil ve
		// birincisini ikincisi gibi bildirmek, yanlış alarmın tanımı
		// olurdu. Ulaşılamayan executor zaten kendi başına görünür
		// (durum ekranı ve journal).
		slog.Warn("disk ölçülemedi — alarm durumu DEĞİŞTİRİLMEDİ",
			"hata", err)
		return
	}

	total := info.GetDiskTotalBytes()
	avail := info.GetDiskAvailableBytes()
	if total == 0 {
		// Sıfır toplam, bölme hatası demek. Ölçüm yok sayılıyor;
		// yukarıdaki gerekçenin aynısı.
		slog.Warn("disk toplamı sıfır okundu — alarm durumu DEĞİŞTİRİLMEDİ")
		return
	}
	free := float64(avail) / float64(total)

	switch action, severity := diskDecision(free); action {
	case diskRaise:
		am.Raise(ctx, diskAlarm(severity, free, avail, total))
	case diskClear:
		am.Clear(ctx, diskAlarmID)
	case diskHold:
		// Histerezis bandı: ne aç ne kapat.
	}
}

// diskAction, ölçümün alarm durumuna ne yapacağını söyler.
type diskAction int

const (
	diskHold diskAction = iota
	diskRaise
	diskClear
)

// diskDecision, boş alan oranından alarm kararını üretir.
//
// ── Neden AYRI ve SAF bir fonksiyon ─────────────────────────────────
//
// Histerezis bu dilimin en kolay sessizce bozulan parçası ve bozulduğunda
// ürettiği şey tam olarak kaçınılmak istenen şey: eşikte salınan bir disk,
// her turda alarm açıp kapatır ve alarmların tamamının güvenilirliğini
// yakar.
//
// `checkDisk`'in içinde gömülü olsaydı sınamak için sahte bir executor
// gerekirdi ve eşik mantığı ölçülmeden kalırdı. Saf fonksiyon, eşiklerin
// TABLO HÂLİNDE sınanmasını mümkün kılıyor.
func diskDecision(free float64) (diskAction, string) {
	switch {
	case free < diskCriticalFree:
		return diskRaise, store.SeverityCritical
	case free < diskWarnFree:
		return diskRaise, store.SeverityWarning
	case free >= diskClearFree:
		// Kapanma eşiği AÇILMA eşiğinden yüksek. Aradaki bant
		// (%15–%20) kasten "ne aç ne kapat" bölgesi.
		return diskClear, ""
	default:
		return diskHold, ""
	}
}

// diskAlarm, disk alarmını kurar.
func diskAlarm(severity string, free float64, avail, total uint64) store.Alarm {
	return store.Alarm{
		ID:       diskAlarmID,
		Kind:     alarm.KindDiskLow,
		Target:   "host",
		Severity: severity,
		Since:    time.Now(),
		Detail: fmt.Sprintf("disk %%%.1f boş (%s / %s)",
			free*100, humanBytes(avail), humanBytes(total)),
	}
}

// humanBytes, bayt sayısını okunur biçime çevirir.
//
// `cmd/panely`'deki ikizinin kopyası: iki binary ayrı ve aralarında
// on satırlık bir biçimlendirici için paket bağımlılığı kurmak,
// kazandırdığından fazlasını maliyet olarak yazardı.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// defaultDiskInterval, disk ölçümleri arasındaki süredir.
//
// Beş dakika, ölçülen şeyin değişim hızıyla orantılı: disk dakikalar
// içinde dolar. Gözetmenin iki saniyelik turuna bağlamak, executor'a
// günde 43.200 gereksiz RPC demekti.
const defaultDiskInterval = 5 * time.Minute

// recordProxyAlarm, açılış uzlaştırmasının sonucunu alarma çevirir.
//
// ── Neden bu koşul en yüksek ciddiyette ─────────────────────────────
//
// Ters vekil uzlaştırılamadıysa systemd `active (running)` gösterir,
// `panely status` sağlıklı der ve BÜTÜN SİTELER KAPALIDIR. Şimdiye
// kadar tek izi bir `slog.Error` satırı ve sdnotify STATUS metniydi —
// yani operatör bakmayı akıl etmedikçe görünmezdi.
//
// ⚠ Yalnızca AÇILIŞTA ölçülüyor. Uzlaştırma açılışta koşuyor, dolayısıyla
// alarm da orada açılıp kapanıyor; sonraki bir bozulmayı bu koşul
// görmez. Kapsamı dar tutmak, olmayan bir sürekli denetimi varmış gibi
// göstermekten iyidir.
func recordProxyAlarm(
	ctx context.Context, am *alarm.Manager, problem string,
) {
	id := alarm.KindProxyUnreconciled + ":host"
	if problem == "" {
		am.Clear(ctx, id)
		return
	}
	am.Raise(ctx, store.Alarm{
		ID:       id,
		Kind:     alarm.KindProxyUnreconciled,
		Target:   "host",
		Severity: store.SeverityCritical,
		Since:    time.Now(),
		// `problem` metni bizim ürettiğimiz sabit bir açıklama
		// (reconcileAtStartup döndürüyor), kullanıcı verisi değil —
		// bu yüzden ayrıntıya yazılabiliyor.
		Detail: "açılışta ters vekil uzlaştırılamadı, TRAFİK AKMIYOR: " +
			problem,
	})
}

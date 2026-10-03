package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/liveness"
	"github.com/erkanrzgc/kadran/internal/sdnotify"
)

// ── Askıda kalma tespiti (K-115) ─────────────────────────────────────
//
// Her arka plan döngüsü kendi ilerleme damgasını tutuyor; watchdog ancak
// hepsi eşik içindeyse ve veritabanı havuzu bir bağlantı verebiliyorsa
// systemd'ye ping atıyor. Ping kesilirse systemd WatchdogSec sonunda
// SIGABRT gönderiyor: Go çalışma zamanı bütün goroutine yığınlarını
// journal'a döküp çıkıyor, Restart=on-failure da kadrand'yi geri
// getiriyor. Takılmanın NEREDE olduğu böylece kayıtta kalıyor.

// minLoopMaxAge, bir döngünün "takıldı" sayılması için gereken EN KISA
// ilerlemesizlik süresi.
//
// ÖLÇÜLMEDİ, koddaki sınırlardan TÜRETİLDİ:
//
//   - executor'a her konteyner çağrısı 60 sn'ye kadar sürebilir
//     (execclient.containerTimeout);
//   - gözetmenin tek bir ziyareti, iyileştirme dahil, birkaç böyle
//     çağrı + 15 sn'lik kapı + uzlaştırıcı kilidini bekleme demek;
//   - vekil izleyicisinin turu 30 sn ile sınırlı, ama önce aynı kilidi
//     bekliyor.
//
// Birkaç dakikalık meşru bir tura en az beş kat pay. Gerçek bir kilitlenme
// sonsuz sürdüğü için büyük eşik yalnızca TESPİTİ geciktirir; küçük eşik
// ise meşru yükte kadrand'yi boş yere yeniden başlatırdı. Gerçek süreler
// watchdog'un saatlik raporunda (liveness.Registry.Gaps).
const minLoopMaxAge = 15 * time.Minute

// loopMaxAge, aralığı `every` olan bir döngünün eşiği: üç tur kaçırmak
// ya da minLoopMaxAge, hangisi uzunsa.
func loopMaxAge(every time.Duration) time.Duration {
	return max(3*every, minLoopMaxAge)
}

// registerLoop, döngü açıksa onu izlemeye alır. Kapalı bir döngüyü
// (aralık 0) kaydetmek, hiç ilerlemeyeceği için watchdog'u eşik dolunca
// kalıcı olarak susturur ve kadrand'yi durmadan yeniden başlatırdı.
func registerLoop(r *liveness.Registry, name string, every time.Duration) *liveness.Beat {
	if every <= 0 {
		return nil
	}
	return r.Register(name, loopMaxAge(every))
}

// watchdogReportEvery, döngülerin en uzun ilerleme aralıklarının günlüğe
// yazılma sıklığı. Eşikleri gerçek sürelerle karşılaştırmanın tek veri
// kaynağı; saatte bir satır journal'ı doldurmaz.
const watchdogReportEvery = time.Hour

// startWatchdog, systemd watchdog istiyorsa pingleyiciyi başlatır.
//
// ⚠ Birim ile ikili BİRLİKTE gitmeli: WatchdogSec taşıyan bir birim altında
// ping atmayan eski bir kadrand her WatchdogSec'te öldürülür. Yalnızca
// ikiliyi geri almak yeniden başlatma döngüsüne döner (K-115).
func startWatchdog(ctx context.Context, beats *liveness.Registry, db liveness.Pinger) {
	every, err := liveness.Interval(os.Getenv, os.Getpid())
	if err != nil {
		slog.Error("watchdog yapılandırması okunamadı — ping ATILMAYACAK, systemd kadrand'yi yeniden başlatacak",
			"hata", err)
		return
	}
	if every == 0 {
		slog.Info("watchdog kapalı: systemd WatchdogSec vermedi")
		return
	}

	// Açılış günlüğündeki bu liste, main'in bir döngüye damga vermeyi
	// unutmadığının tek kanıtı (Beat.Mark nil'de sessiz).
	slog.Info("watchdog açık",
		"ping_araligi", every,
		"izlenen_donguler", strings.Join(beats.Names(), ","))

	w := &liveness.Watchdog{
		Beats:        beats,
		DB:           db,
		Notify:       func() error { return sdnotify.Send("WATCHDOG=1") },
		Every:        every,
		ProbeTimeout: every / 3,
		ReportEvery:  watchdogReportEvery,
	}
	go w.Run(ctx)
}

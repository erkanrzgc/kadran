package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/deploy"
	"github.com/erkanrzgc/kadran/internal/store"
)

type sahteOnarici struct {
	sonuclar []onarimSonucu
	i        int
}

type onarimSonucu struct {
	eksik   []string
	atlanan map[string]string
	tam     bool // canlı, beklenenle İKİ YÖNLÜ birebir
	err     error
}

func (s *sahteOnarici) Repair(context.Context) (deploy.RepairResult, error) {
	r := s.sonuclar[s.i]
	s.i++
	return deploy.RepairResult{Missing: r.eksik, Skipped: r.atlanan, Exact: r.tam}, r.err
}

type sahteAlarm struct {
	acilan  []store.Alarm
	kapanan []string
}

func (a *sahteAlarm) Raise(_ context.Context, al store.Alarm) { a.acilan = append(a.acilan, al) }
func (a *sahteAlarm) Clear(_ context.Context, id string)      { a.kapanan = append(a.kapanan, id) }

func izleyiciKos(t *testing.T, sonuclar ...onarimSonucu) *sahteAlarm {
	t.Helper()
	am := &sahteAlarm{}
	w := &proxyWatcher{rp: &sahteOnarici{sonuclar: sonuclar}, am: am}
	for range sonuclar {
		w.tick(context.Background())
	}
	return am
}

var hata = onarimSonucu{err: errors.New("admin soketine ulaşılamadı")}

// TestProxyWatcherAlarmsOnlyAfterRepeatedFailures: tek bir başarısız tur
// alarm değil (Caddy yeniden başlarken admin soketi saniyeler içinde
// kapalı olabilir); üst üste üç tur alarm.
func TestProxyWatcherAlarmsOnlyAfterRepeatedFailures(t *testing.T) {
	if am := izleyiciKos(t, hata, hata); len(am.acilan) != 0 {
		t.Errorf("iki başarısız turda alarm açıldı: %v", am.acilan)
	}
	am := izleyiciKos(t, hata, hata, hata)
	if len(am.acilan) != 1 {
		t.Fatalf("üç başarısız turda %d alarm, 1 bekleniyordu", len(am.acilan))
	}
	a := am.acilan[0]
	if a.ID != proxyAlarmID || a.Severity != store.SeverityCritical {
		t.Errorf("yanlış alarm: %+v", a)
	}
	if !strings.Contains(a.Detail, "3") {
		t.Errorf("ayrıntı deneme sayısını söylemiyor: %q", a.Detail)
	}
}

// TestProxyWatcherClearsWhatItRaised: kendi açtığı alarmı, vekil yeniden
// cevap verip canlı birebir olunca kapatır.
func TestProxyWatcherClearsWhatItRaised(t *testing.T) {
	am := izleyiciKos(t, hata, hata, hata, onarimSonucu{tam: true})
	if len(am.kapanan) != 1 || am.kapanan[0] != proxyAlarmID {
		t.Errorf("düzelince alarm kapanmadı: %v", am.kapanan)
	}
}

// TestProxyWatcherClearsAfterARealRepair: rotaları GERİ YÜKLEDİYSE (Load
// iki yönlü doğruladı) trafik akıyor; açılış alarmı kapanır.
func TestProxyWatcherClearsAfterARealRepair(t *testing.T) {
	am := izleyiciKos(t, onarimSonucu{eksik: []string{"blog.example.com"}, tam: true})
	if len(am.kapanan) != 1 {
		t.Errorf("onarımdan sonra alarm kapanmadı: %v", am.kapanan)
	}
}

// TestProxyWatcherClearsTheStartupAlarmOnceTheAppIsRouted: taze sunucuda
// reboot'tan sonra ölçülen hâl (K-112). Açılışta konteyner hazır
// değildi, uygulama atlandı, alarm açıldı; gözetmen 11 sn sonra
// iyileştirip rotayı yükledi. İzleyici hiçbir şey onarmadı — ve alarm
// dakikalarca AÇIK kaldı. Artık: atlanan kalmadı ve canlı birebir →
// kapanır.
func TestProxyWatcherClearsTheStartupAlarmOnceTheAppIsRouted(t *testing.T) {
	atlanan := map[string]string{"web": "aktif sürümün ayakta replikası yok"}
	am := izleyiciKos(t, onarimSonucu{atlanan: atlanan}, onarimSonucu{tam: true})
	if len(am.kapanan) != 1 || am.kapanan[0] != proxyAlarmID {
		t.Errorf("uygulama rotalandıktan sonra açılış alarmı kapanmadı: %v", am.kapanan)
	}
}

// TestProxyWatcherKeepsAlarmWhenLiveDiffers: eksik yok, atlanan yok ama
// canlı birebir DEĞİL — admin soketine başkası yazmış olabilir (K-054).
// Açılış alarmı tam da bunu diyor olabilir; "eksik yok, atlanan yok"
// diye kapatmak onu sessizce gizlerdi.
func TestProxyWatcherKeepsAlarmWhenLiveDiffers(t *testing.T) {
	if am := izleyiciKos(t, onarimSonucu{}, onarimSonucu{}); len(am.kapanan) != 0 {
		t.Errorf("canlı birebir değilken alarm kapandı: %v", am.kapanan)
	}
	if am := izleyiciKos(t, hata, hata, hata, onarimSonucu{}); len(am.kapanan) != 0 {
		t.Errorf("kendi alarmı, canlı birebir değilken kapandı: %v", am.kapanan)
	}
}

// TestProxyWatcherKeepsAlarmWhileAppsAreSkipped: canlı birebir olsa bile,
// o an rotalanamayan bir uygulama varken alarm KAPANMAZ. Güvenlik
// incelemesi buldu: ilk hâli onarımdan sonra koşulsuz kapatıyordu ve
// açılıştaki "rotalanamayan uygulama var" alarmını gizleyebiliyordu.
func TestProxyWatcherKeepsAlarmWhileAppsAreSkipped(t *testing.T) {
	atlanan := map[string]string{"shop": "ayakta replika yok"}
	am := izleyiciKos(t, onarimSonucu{eksik: []string{"blog.example.com"}, atlanan: atlanan, tam: true})
	if len(am.kapanan) != 0 {
		t.Errorf("atlanan uygulama varken alarm kapandı: %v", am.kapanan)
	}
	am = izleyiciKos(t, hata, hata, hata, onarimSonucu{atlanan: atlanan, tam: true})
	if len(am.kapanan) != 0 {
		t.Errorf("kendi alarmı, atlanan uygulama varken kapandı: %v", am.kapanan)
	}
}

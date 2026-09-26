package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/erkanrzgc/panely/internal/deploy"
	"github.com/erkanrzgc/panely/internal/store"
)

type sahteOnarici struct {
	sonuclar []onarimSonucu
	i        int
}

type onarimSonucu struct {
	eksik   []string
	atlanan map[string]string
	err     error
}

func (s *sahteOnarici) Repair(context.Context) (deploy.RepairResult, error) {
	r := s.sonuclar[s.i]
	s.i++
	return deploy.RepairResult{Missing: r.eksik, Skipped: r.atlanan}, r.err
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
// cevap verince kapatır.
func TestProxyWatcherClearsWhatItRaised(t *testing.T) {
	am := izleyiciKos(t, hata, hata, hata, onarimSonucu{})
	if len(am.kapanan) != 1 || am.kapanan[0] != proxyAlarmID {
		t.Errorf("düzelince alarm kapanmadı: %v", am.kapanan)
	}
}

// TestProxyWatcherClearsAfterARealRepair: rotaları GERİ YÜKLEDİYSE trafik
// akıyor demektir; açılışta kalmış "trafik akmıyor" alarmı kapanır.
func TestProxyWatcherClearsAfterARealRepair(t *testing.T) {
	am := izleyiciKos(t, onarimSonucu{eksik: []string{"blog.example.com"}})
	if len(am.kapanan) != 1 {
		t.Errorf("onarımdan sonra alarm kapanmadı: %v", am.kapanan)
	}
}

// TestProxyWatcherLeavesStartupAlarmAlone: hiçbir şey onarmadığı ve kendisi
// alarm açmadığı bir turda alarma DOKUNMAZ. Açılış alarmı "rotalanamayan
// uygulama var" da diyebilir; izleyici her şeyi yerinde görse bile o
// uygulama hâlâ atlanıyor olabilir — körlemesine kapatmak onu gizlerdi.
func TestProxyWatcherLeavesStartupAlarmAlone(t *testing.T) {
	if am := izleyiciKos(t, onarimSonucu{}, onarimSonucu{}); len(am.kapanan) != 0 {
		t.Errorf("izleyici kendisinin olmayan alarmı kapattı: %v", am.kapanan)
	}
}

// TestProxyWatcherKeepsAlarmWhileAppsAreSkipped: rotaları geri yükleyen
// bir onarım bile, o an rotalanamayan bir uygulama varken alarmı
// KAPATMAZ. Güvenlik incelemesi buldu: ilk hâli onarımdan sonra
// koşulsuz kapatıyordu ve açılıştaki "rotalanamayan uygulama var"
// alarmını gizleyebiliyordu.
func TestProxyWatcherKeepsAlarmWhileAppsAreSkipped(t *testing.T) {
	atlanan := map[string]string{"shop": "ayakta replika yok"}
	am := izleyiciKos(t, onarimSonucu{eksik: []string{"blog.example.com"}, atlanan: atlanan})
	if len(am.kapanan) != 0 {
		t.Errorf("atlanan uygulama varken alarm kapandı: %v", am.kapanan)
	}
	am = izleyiciKos(t, hata, hata, hata, onarimSonucu{atlanan: atlanan})
	if len(am.kapanan) != 0 {
		t.Errorf("kendi alarmı, atlanan uygulama varken kapandı: %v", am.kapanan)
	}
}

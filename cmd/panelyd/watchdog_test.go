package main

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/deploy"
	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/liveness"
)

// Eşikler SABİTTEN okunmuyor: beklenti kendi kendine referans verseydi
// çarpan ya da taban değişince test onunla birlikte kayardı.
func TestLoopMaxAge(t *testing.T) {
	cases := []struct {
		every, want time.Duration
	}{
		{2 * time.Second, 15 * time.Minute},  // gözetmen: taban
		{10 * time.Second, 15 * time.Minute}, // vekil izleyicisi: taban
		{5 * time.Minute, 15 * time.Minute},  // disk: üç tur = taban
		{time.Hour, 3 * time.Hour},           // yedek: üç tur
	}
	for _, c := range cases {
		if got := loopMaxAge(c.every); got != c.want {
			t.Errorf("loopMaxAge(%s) = %s, beklenen %s", c.every, got, c.want)
		}
	}
}

// Kapalı döngü kaydedilirse hiç ilerlemez; eşik dolunca watchdog susar ve
// panelyd durmadan yeniden başlardı.
func TestRegisterLoopSkipsDisabledLoops(t *testing.T) {
	r := liveness.NewRegistry(time.Now)

	if b := registerLoop(r, "yedek", 0); b != nil {
		t.Fatal("kapalı döngü için damga döndü")
	}
	registerLoop(r, "disk", time.Minute)

	if got := strings.Join(r.Names(), ","); got != "disk" {
		t.Fatalf("kayıtlı döngüler = %q, beklenen yalnız disk", got)
	}
}

// ── Döngüler damgayı GERÇEKTEN tazeliyor mu ─────────────────────────
//
// Watchdog'un en ucuz bozulma biçimi, bir döngüden Mark çağrısının
// silinmesi: derleyici bir şey demez, sayaç yeşil kalır, watchdog o
// döngünün takılmasını bir daha asla görmez. Bu testler gerçek döngü
// fonksiyonlarını koşturuyor.

type ileriSaat struct{ t atomic.Int64 }

func (s *ileriSaat) now() time.Time          { return time.Unix(0, s.t.Load()) }
func (s *ileriSaat) ilerlet(d time.Duration) { s.t.Add(int64(d)) }

// tazelenmesiniBekle, damga yeniden taze olana kadar bekler.
func tazelenmesiniBekle(t *testing.T, r *liveness.Registry, ne string) {
	t.Helper()
	son := time.Now().Add(3 * time.Second)
	for time.Now().Before(son) {
		if len(r.Stale()) == 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("%s: döngü damgayı tazelemedi: %v", ne, r.Stale())
}

// dongu, bir döngüyü koşturup iki ayrı tazelemeyi sınar: önce ilk tur,
// sonra saat yeniden ileri alınınca ticker turu. Tek bir bekleme,
// iki Mark'tan birinin silinmesini göremezdi.
func dongu(t *testing.T, kos func(ctx context.Context, b *liveness.Beat)) {
	t.Helper()
	saat := &ileriSaat{}
	saat.t.Store(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC).UnixNano())
	r := liveness.NewRegistry(saat.now)
	b := r.Register("döngü", time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	bitti := make(chan struct{})
	t.Cleanup(func() { cancel(); <-bitti })

	saat.ilerlet(time.Hour)
	if len(r.Stale()) != 1 {
		t.Fatal("kontrol: saat ileri alındı ama damga bayat görünmüyor")
	}
	go func() { kos(ctx, b); close(bitti) }()
	tazelenmesiniBekle(t, r, "ilk tur")

	saat.ilerlet(time.Hour)
	tazelenmesiniBekle(t, r, "ticker turu")
}

type hepTamOnarici struct{}

func (hepTamOnarici) Repair(context.Context) (deploy.RepairResult, error) {
	return deploy.RepairResult{Exact: true}, nil
}

func TestProxyWatchLoopMarksProgress(t *testing.T) {
	dongu(t, func(ctx context.Context, b *liveness.Beat) {
		watchProxy(ctx, &proxyWatcher{rp: hepTamOnarici{}, am: &sahteAlarm{}}, 5*time.Millisecond, b)
	})
}

func TestDiskLoopMarksProgress(t *testing.T) {
	am, _, _ := gercekAlarmlar(t)
	// Olmayan executor: ölçüm hata verir, alarm açılmaz, döngü sürer.
	exec, err := execclient.Dial(filepath.Join(t.TempDir(), "olmayan.sock"))
	if err != nil {
		t.Fatalf("executor istemcisi kurulamadı: %v", err)
	}
	t.Cleanup(func() { _ = exec.Close() })

	dongu(t, func(ctx context.Context, b *liveness.Beat) {
		watchDisk(ctx, exec, am, 5*time.Millisecond, b)
	})
}

func TestBackupLoopMarksProgress(t *testing.T) {
	am, db, _ := gercekAlarmlar(t)

	dongu(t, func(ctx context.Context, b *liveness.Beat) {
		runBackupScheduler(ctx, db, am, 5*time.Millisecond, b)
	})
}

// acilisTuru, aralık bir saatken yalnızca AÇILIŞTAKİ turun damgayı
// tazeleyip tazelemediğini sınar. Yukarıdaki kısa aralıklı testte ticker
// ilk turu da tazelediği için oradaki ilk bekleme bunu göremezdi.
func acilisTuru(t *testing.T, kos func(ctx context.Context, b *liveness.Beat)) {
	t.Helper()
	saat := &ileriSaat{}
	saat.t.Store(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC).UnixNano())
	r := liveness.NewRegistry(saat.now)
	b := r.Register("döngü", time.Minute)

	ctx, cancel := context.WithCancel(context.Background())
	bitti := make(chan struct{})
	t.Cleanup(func() { cancel(); <-bitti })

	saat.ilerlet(time.Hour)
	go func() { kos(ctx, b); close(bitti) }()
	tazelenmesiniBekle(t, r, "açılış turu")
}

func TestDiskLoopMarksAtStartup(t *testing.T) {
	am, _, _ := gercekAlarmlar(t)
	exec, err := execclient.Dial(filepath.Join(t.TempDir(), "olmayan.sock"))
	if err != nil {
		t.Fatalf("executor istemcisi kurulamadı: %v", err)
	}
	t.Cleanup(func() { _ = exec.Close() })

	acilisTuru(t, func(ctx context.Context, b *liveness.Beat) {
		watchDisk(ctx, exec, am, time.Hour, b)
	})
}

func TestBackupLoopMarksAtStartup(t *testing.T) {
	am, db, _ := gercekAlarmlar(t)

	acilisTuru(t, func(ctx context.Context, b *liveness.Beat) {
		runBackupScheduler(ctx, db, am, time.Hour, b)
	})
}

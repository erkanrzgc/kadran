package liveness

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/store"
)

// sahteSaat, elle ilerletilen bir saat.
type sahteSaat struct{ t atomic.Int64 }

func yeniSaat() *sahteSaat {
	s := &sahteSaat{}
	s.t.Store(time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC).UnixNano())
	return s
}

func (s *sahteSaat) Now() time.Time          { return time.Unix(0, s.t.Load()) }
func (s *sahteSaat) ilerlet(d time.Duration) { s.t.Add(int64(d)) }

type sahtePing struct{ err error }

func (p sahtePing) PingContext(context.Context) error { return p.err }

// sayac, Notify çağrılarını sayar.
type sayac struct{ n atomic.Int32 }

func (s *sayac) notify() error { s.n.Add(1); return nil }

func bekci(r *Registry, db Pinger, s *sayac) *Watchdog {
	return &Watchdog{Beats: r, DB: db, Notify: s.notify, Every: time.Second, ProbeTimeout: 50 * time.Millisecond}
}

func TestFreshLoopsAndHealthyDatabasePing(t *testing.T) {
	saat := yeniSaat()
	r := NewRegistry(saat.Now)
	r.Register("gözetmen", time.Minute)
	r.Register("vekil", time.Minute)
	var s sayac

	bekci(r, sahtePing{}, &s).beat(context.Background())

	if s.n.Load() != 1 {
		t.Fatalf("sağlıklıyken ping atılmadı: %d", s.n.Load())
	}
}

// Kayıt anında damga taze: ticker'lar ilk kez bir aralık SONRA
// tetikleniyor ve boş damga döngüyü açılışta "takılmış" gösterirdi.
func TestRegisterStartsFresh(t *testing.T) {
	r := NewRegistry(yeniSaat().Now)
	r.Register("yedek", time.Second)

	if st := r.Stale(); len(st) != 0 {
		t.Fatalf("hiç Mark olmadan kayıt taze olmalı: %v", st)
	}
}

func TestStaleLoopWithholdsPing(t *testing.T) {
	saat := yeniSaat()
	r := NewRegistry(saat.Now)
	r.Register("gözetmen", time.Minute)
	vekil := r.Register("vekil", 10*time.Minute)
	var s sayac
	w := bekci(r, sahtePing{}, &s)

	saat.ilerlet(2 * time.Minute)
	vekil.Mark()

	err := w.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "gözetmen") {
		t.Fatalf("eşiğini aşan döngü adıyla bildirilmedi: %v", err)
	}
	if strings.Contains(err.Error(), "vekil") {
		t.Errorf("taze döngü de bayat sayıldı: %v", err)
	}
	w.beat(context.Background())
	if s.n.Load() != 0 {
		t.Fatalf("bayat döngü varken ping atıldı")
	}
}

// Sınır: tam eşikte taze, bir nanosaniye sonra bayat.
func TestThresholdBoundary(t *testing.T) {
	saat := yeniSaat()
	r := NewRegistry(saat.Now)
	r.Register("disk", time.Minute)

	saat.ilerlet(time.Minute)
	if st := r.Stale(); len(st) != 0 {
		t.Fatalf("tam eşikte bayat sayıldı: %v", st)
	}
	saat.ilerlet(time.Nanosecond)
	if st := r.Stale(); len(st) != 1 {
		t.Fatalf("eşik aşıldı ama bayat sayılmadı: %v", st)
	}
}

func TestMarkRefreshesTheLoop(t *testing.T) {
	saat := yeniSaat()
	r := NewRegistry(saat.Now)
	b := r.Register("gözetmen", time.Minute)

	saat.ilerlet(time.Hour)
	b.Mark()

	if st := r.Stale(); len(st) != 0 {
		t.Fatalf("Mark damgayı tazelemedi: %v", st)
	}
}

// nil alıcı: izlenmeyen bir döngü (testte, watchdog kapalıyken) Mark
// çağırabilmeli.
func TestMarkOnNilBeatIsANoop(t *testing.T) {
	var b *Beat
	b.Mark()
}

func TestDatabaseProbeFailureWithholdsPing(t *testing.T) {
	r := NewRegistry(yeniSaat().Now)
	r.Register("gözetmen", time.Minute)
	var s sayac
	w := bekci(r, sahtePing{err: errors.New("havuz dolu")}, &s)

	w.beat(context.Background())

	if s.n.Load() != 0 {
		t.Fatalf("veritabanı yoklaması düştü ama ping atıldı")
	}
	if err := w.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "havuz dolu") {
		t.Fatalf("yoklama hatası sebep olarak taşınmadı: %v", err)
	}
}

// bekleyenPing, bağlam bitene kadar döner — tükenmiş havuzun davranışı.
type bekleyenPing struct{}

func (bekleyenPing) PingContext(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestDatabaseProbeHasItsOwnTimeout(t *testing.T) {
	r := NewRegistry(yeniSaat().Now)
	var s sayac
	w := bekci(r, bekleyenPing{}, &s)

	basla := time.Now()
	err := w.Check(context.Background())
	gecen := time.Since(basla)

	if err == nil {
		t.Fatal("hiç dönmeyen yoklama başarı sayıldı")
	}
	if gecen > 2*time.Second {
		t.Fatalf("yoklama kendi sınırında kesilmedi: %s", gecen)
	}
}

// TestPingDetectsAnExhaustedPool, yoklamanın DAYANDIĞI varsayımı gerçek
// veritabanında sınar: havuz tek bağlantılı olduğu için PingContext boş
// bir bağlantı BEKLİYOR. Havuz ikiye çıkarılırsa yoklama tükenmeyi
// göremez ve bu test kırmızıya döner.
func TestPingDetectsAnExhaustedPool(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(ctx, filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Pozitif kontrol: boştayken yoklama geçmeli, yoksa aşağıdaki
	// "düştü" hiçbir şey kanıtlamaz.
	if err := ping(db.DB(), 200*time.Millisecond); err != nil {
		t.Fatalf("boş havuzda yoklama düştü: %v", err)
	}

	tx, err := db.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("işlem açılamadı: %v", err)
	}
	if err := ping(db.DB(), 200*time.Millisecond); err == nil {
		t.Fatal("tek bağlantı işlemdeyken yoklama geçti — tükenmiş havuz görünmüyor")
	}

	_ = tx.Rollback()
	if err := ping(db.DB(), 200*time.Millisecond); err != nil {
		t.Fatalf("bağlantı geri verildi ama yoklama düşüyor: %v", err)
	}
}

func ping(p Pinger, d time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return p.PingContext(ctx)
}

func TestIntervalFollowsTheSystemdContract(t *testing.T) {
	const pid = 4242
	cases := []struct {
		ad   string
		env  map[string]string
		want time.Duration
		hata bool
	}{
		{"watchdog kapalı", map[string]string{}, 0, false},
		{"yarı aralık", map[string]string{"WATCHDOG_USEC": "60000000"}, 30 * time.Second, false},
		{"PID bu süreç", map[string]string{"WATCHDOG_USEC": "60000000", "WATCHDOG_PID": "4242"}, 30 * time.Second, false},
		// Değişken bir üst süreçten miras kalmış: ping başkası adına gider.
		{"PID başka süreç", map[string]string{"WATCHDOG_USEC": "60000000", "WATCHDOG_PID": "1"}, 0, false},
		{"sıfır", map[string]string{"WATCHDOG_USEC": "0"}, 0, false},
		{"bozuk süre", map[string]string{"WATCHDOG_USEC": "altmış"}, 0, true},
		{"bozuk PID", map[string]string{"WATCHDOG_USEC": "60000000", "WATCHDOG_PID": "x"}, 0, true},
	}
	for _, c := range cases {
		t.Run(c.ad, func(t *testing.T) {
			got, err := Interval(func(k string) string { return c.env[k] }, pid)
			if (err != nil) != c.hata {
				t.Fatalf("hata = %v, beklenen hata: %v", err, c.hata)
			}
			if got != c.want {
				t.Fatalf("aralık = %s, beklenen %s", got, c.want)
			}
		})
	}
}

// Seyrek rapor: iki Mark arasındaki EN UZUN süre raporlanıyor ve
// sıfırlanıyor. Eşikleri gerçek sürelerden seçmenin tek veri kaynağı bu.
func TestGapsReportTheLongestIntervalAndReset(t *testing.T) {
	saat := yeniSaat()
	r := NewRegistry(saat.Now)
	b := r.Register("vekil", time.Hour)

	saat.ilerlet(10 * time.Second)
	b.Mark()
	saat.ilerlet(40 * time.Second)
	b.Mark()
	saat.ilerlet(20 * time.Second)
	b.Mark()

	got := r.Gaps()
	if len(got) != 1 || got[0] != "vekil=40s" {
		t.Fatalf("en uzun aralık = %v, beklenen [vekil=40s]", got)
	}
	if again := r.Gaps(); len(again) != 1 || again[0] != "vekil=0s" {
		t.Fatalf("rapordan sonra sıfırlanmadı: %v", again)
	}
}

// Sürmekte olan aralık da sayılmalı: canlıdaki ilk rapor (K-118) yedek
// döngüsü için "0s" dedi, çünkü 1 saatlik aralık rapordan hemen sonra
// kapanacaktı. Aynı kusur, şu an takılmakta olan ama eşiğe henüz
// varmamış bir döngüyü de gizlerdi.
func TestGapsIncludeTheOngoingInterval(t *testing.T) {
	saat := yeniSaat()
	r := NewRegistry(saat.Now)
	r.Register("yedek", 3*time.Hour)
	b := r.Register("vekil", time.Hour)

	saat.ilerlet(10 * time.Second)
	b.Mark()
	saat.ilerlet(40 * time.Minute)

	got := strings.Join(r.Gaps(), " ")
	if got != "yedek=40m10s vekil=40m0s" {
		t.Fatalf("aralıklar = %q, beklenen sürmekte olanlar dahil", got)
	}
}

func TestNamesListsRegisteredLoops(t *testing.T) {
	r := NewRegistry(yeniSaat().Now)
	r.Register("gözetmen", time.Minute)
	r.Register("vekil", time.Minute)

	if got := strings.Join(r.Names(), ","); got != "gözetmen,vekil" {
		t.Fatalf("kayıtlı döngüler = %q", got)
	}
}

// Run gerçek bir ticker'la koşuyor ve bağlam bitince dönmeli.
func TestRunPingsUntilCancelled(t *testing.T) {
	r := NewRegistry(time.Now)
	r.Register("gözetmen", time.Hour)
	var s sayac
	w := &Watchdog{Beats: r, DB: sahtePing{}, Notify: s.notify, Every: 5 * time.Millisecond, ProbeTimeout: time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	bitti := make(chan struct{})
	go func() { w.Run(ctx); close(bitti) }()

	deadline := time.Now().Add(2 * time.Second)
	for s.n.Load() < 3 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cancel()
	select {
	case <-bitti:
	case <-time.After(2 * time.Second):
		t.Fatal("Run bağlam iptalinde dönmedi")
	}
	if s.n.Load() < 3 {
		t.Fatalf("Run ping atmadı: %d", s.n.Load())
	}
}

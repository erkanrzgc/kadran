// Package liveness, panelyd'nin "süreç ayakta ama iş yapmıyor" hâlini
// systemd'nin watchdog'una bağlar (K-115).
//
// ── Neden ayrı bir sayaç goroutine'i DEĞİL ──────────────────────────
//
// En kolay uygulama, sabit aralıkla WATCHDOG=1 gönderen bir goroutine.
// O yalnızca SÜRECİN TAMAMI donduğunda susar. Kilitlenmiş bir döngü ya da
// takılmış bir kilit varken sayaç ping atmaya devam eder ve "askıda kalma
// tespit ediliyor" iddiası kodun sahip olmadığı bir özelliği anlatırdı.
//
// Burada ping ancak iki koşulla gidiyor:
//
//   - kayıtlı her döngü kendi eşiği içinde İLERLEMİŞ (Beat.Mark);
//   - veritabanı havuzundan kısa sürede bir bağlantı alınabiliyor.
//     Havuz tek bağlantılı: tükenmiş bir havuz bütün RPC'leri bekletir.
//
// ── Neyi GÖRMEZ ─────────────────────────────────────────────────────
//
// Hiçbir döngünün ve yoklamanın dokunmadığı bir şeye kilitlenmiş tek bir
// RPC işleyicisi. gRPC sunucusu her isteği ayrı goroutine'de koşturuyor;
// o işleyici takılırken geri kalan her şey ilerler.
package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Registry, izlenen döngülerin ilerleme damgalarını tutar.
type Registry struct {
	now   func() time.Time
	mu    sync.Mutex
	beats []*Beat
}

// NewRegistry, verilen saatle bir kayıt defteri kurar.
func NewRegistry(now func() time.Time) *Registry {
	return &Registry{now: now}
}

// Beat, tek bir döngünün son ilerleme anı.
type Beat struct {
	name   string
	maxAge time.Duration
	now    func() time.Time
	last   atomic.Int64 // UnixNano
	maxGap atomic.Int64 // son rapordan beri iki Mark arası en uzun süre (ns)
}

// Register, bir döngüyü izlemeye alır.
//
// Damga ŞİMDİ ile başlıyor: döngülerin ticker'ları ilk kez bir aralık
// sonra tetikleniyor ve boş bir damga döngüyü açılışta "takılmış"
// gösterirdi.
func (r *Registry) Register(name string, maxAge time.Duration) *Beat {
	b := &Beat{name: name, maxAge: maxAge, now: r.now}
	b.last.Store(r.now().UnixNano())

	r.mu.Lock()
	defer r.mu.Unlock()
	r.beats = append(r.beats, b)
	return b
}

// Mark, döngünün ilerlediğini kaydeder.
//
// nil alıcıyla hiçbir şey yapmaz: watchdog kapalıyken ya da testte
// döngüler damgasız koşabilsin. Bu kolaylığın bedeli, main'in bir
// döngüye damga vermeyi UNUTMASININ derleyiciye görünmemesi; bu yüzden
// panelyd açılışta izlenen döngülerin adlarını günlüğe yazıyor.
func (b *Beat) Mark() {
	if b == nil {
		return
	}
	now := b.now().UnixNano()
	gap := now - b.last.Swap(now)
	for {
		cur := b.maxGap.Load()
		if gap <= cur || b.maxGap.CompareAndSwap(cur, gap) {
			return
		}
	}
}

// Stale, eşiğini aşmış döngüleri okunabilir biçimde döndürür.
func (r *Registry) Stale() []string {
	now := r.now()
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []string
	for _, b := range r.beats {
		age := now.Sub(time.Unix(0, b.last.Load()))
		if age > b.maxAge {
			out = append(out, fmt.Sprintf("%s (%s önce, eşik %s)",
				b.name, age.Round(time.Second), b.maxAge))
		}
	}
	return out
}

// Gaps, son çağrıdan beri her döngünün iki ilerlemesi arasındaki EN UZUN
// süreyi döndürür ve sayaçları sıfırlar.
//
// Eşikler koddaki zaman sınırlarından TÜRETİLDİ, ölçülmedi (K-115). Bu
// rapor, onları gerçek sürelerle karşılaştırmanın tek veri kaynağı.
func (r *Registry) Gaps() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.beats))
	for _, b := range r.beats {
		out = append(out, fmt.Sprintf("%s=%s", b.name, time.Duration(b.maxGap.Swap(0)).Round(time.Second)))
	}
	return out
}

// Names, izlenen döngülerin adlarını kayıt sırasıyla döndürür.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]string, 0, len(r.beats))
	for _, b := range r.beats {
		out = append(out, b.name)
	}
	return out
}

// Pinger, veritabanı havuzundan bir bağlantı alınabildiğini sınar.
//
// *sql.DB'nin PingContext'i havuz doluysa boş bir bağlantı BEKLİYOR;
// tükenmeyi görebilmesinin tek sebebi bu (TestPingDetectsAnExhaustedPool).
type Pinger interface {
	PingContext(ctx context.Context) error
}

// Watchdog, canlılık koşulları sağlandıkça systemd'ye ping atar.
type Watchdog struct {
	Beats *Registry
	DB    Pinger
	// Notify, systemd'ye WATCHDOG=1 gönderir.
	Notify func() error
	// Every, ping aralığı: WatchdogSec'in yarısı (bkz. Interval).
	Every time.Duration
	// ProbeTimeout, veritabanı yoklamasının sınırı; Every'den belirgin
	// biçimde kısa olmalı, yoksa tek bir yavaş yoklama iki pingi yer.
	ProbeTimeout time.Duration
	// ReportEvery, döngü aralıklarının günlüğe yazılma sıklığı (0 = hiç).
	ReportEvery time.Duration
}

// Check, tek bir değerlendirme yapar; nil canlı demektir.
func (w *Watchdog) Check(ctx context.Context) error {
	var sorunlar []string
	if stale := w.Beats.Stale(); len(stale) > 0 {
		sorunlar = append(sorunlar, "ilerlemeyen döngü: "+strings.Join(stale, ", "))
	}

	probe, cancel := context.WithTimeout(ctx, w.ProbeTimeout)
	defer cancel()
	if err := w.DB.PingContext(probe); err != nil {
		sorunlar = append(sorunlar, "veritabanından bağlantı alınamadı: "+err.Error())
	}

	if len(sorunlar) > 0 {
		return errors.New(strings.Join(sorunlar, "; "))
	}
	return nil
}

// Run, bağlam bitene kadar her Every'de bir değerlendirip ping atar.
//
// Tek bir başarısız değerlendirme yalnızca BİR pingi atlatır. systemd
// süreci ancak WatchdogSec boyunca HİÇ ping gelmezse öldürüyor; Every
// onun yarısı olduğu için bu en az iki ardışık başarısızlık demek.
func (w *Watchdog) Run(ctx context.Context) {
	t := time.NewTicker(w.Every)
	defer t.Stop()
	lastReport := w.Beats.now()

	w.beat(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.beat(ctx)
			if w.ReportEvery > 0 && w.Beats.now().Sub(lastReport) >= w.ReportEvery {
				lastReport = w.Beats.now()
				slog.Info("watchdog: döngülerin en uzun ilerleme aralıkları",
					"aralik", strings.Join(w.Beats.Gaps(), " "))
			}
		}
	}
}

func (w *Watchdog) beat(ctx context.Context) {
	if err := w.Check(ctx); err != nil {
		slog.Error("watchdog: canlılık BİLDİRİLMİYOR — sürerse systemd panelyd'yi yeniden başlatacak",
			"sebep", err)
		return
	}
	if err := w.Notify(); err != nil {
		slog.Warn("watchdog: systemd'ye bildirilemedi", "hata", err)
	}
}

// Interval, systemd'nin ortam sözleşmesinden ping aralığını çıkarır
// (sd_watchdog_enabled(3)). 0, watchdog'un bu süreç için KAPALI olduğu
// demek.
//
// WATCHDOG_PID varsa ve bu süreç değilse de kapalı: değişken bir üst
// süreçten miras kalmış olabilir ve ping başkası adına giderdi.
func Interval(getenv func(string) string, pid int) (time.Duration, error) {
	raw := getenv("WATCHDOG_USEC")
	if raw == "" {
		return 0, nil
	}
	usec, err := strconv.ParseUint(raw, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("liveness: WATCHDOG_USEC çözümlenemedi: %w", err)
	}
	if p := getenv("WATCHDOG_PID"); p != "" {
		owner, err := strconv.Atoi(p)
		if err != nil {
			return 0, fmt.Errorf("liveness: WATCHDOG_PID çözümlenemedi: %w", err)
		}
		if owner != pid {
			return 0, nil
		}
	}
	return time.Duration(usec) * time.Microsecond / 2, nil
}

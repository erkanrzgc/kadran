package deploy

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/erkanrzgc/panely/internal/execclient"
	"github.com/erkanrzgc/panely/internal/store"
)

// Lifecycle, konteyner yaşam döngüsünü yürütür.
type Lifecycle interface {
	EnsureNetwork(ctx context.Context, appID string) (string, error)
	CreateReplica(ctx context.Context, opts execclient.CreateReplicaOptions) error
	StartReplica(ctx context.Context, appID, releaseID string, index uint32) error
	ListReplicas(ctx context.Context, appID string) ([]execclient.Replica, error)
}

// Activations, trafiğin hangi sürüme gittiğini yazar.
type Activations interface {
	SetActiveRelease(ctx context.Context, appID, releaseID string) error
}

// Clock, bekleme davranışını sınanabilir kılar.
type Clock interface {
	Sleep(ctx context.Context, d time.Duration) error
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Rollout, derlenmiş bir sürümü canlıya alır.
type Rollout struct {
	lifecycle Lifecycle
	store     Activations
	rec       *Reconciler
	clock     Clock

	gate GateOptions
}

// GateOptions, trafiğin devredilmesinden ÖNCEKİ bekleme ölçütüdür.
type GateOptions struct {
	// Successes, arka arkaya kaç ölçümde replikaların HAZIR görülmesi
	// gerektiği.
	Successes int
	// Interval, ölçümler arası bekleme.
	Interval time.Duration
	// Timeout, kapının toplam süre sınırı.
	Timeout time.Duration
}

// DefaultGate, makul varsayılanlar.
var DefaultGate = GateOptions{Successes: 3, Interval: 2 * time.Second, Timeout: 90 * time.Second}

// NewRollout, dağıtım orkestratörünü kurar.
func NewRollout(l Lifecycle, a Activations, rec *Reconciler, gate GateOptions) (*Rollout, error) {
	if l == nil || a == nil || rec == nil {
		return nil, errors.New("deploy: yaşam döngüsü, aktivasyon ve uzlaştırıcı zorunlu")
	}
	if gate.Successes <= 0 || gate.Interval <= 0 || gate.Timeout <= 0 {
		return nil, errors.New("deploy: sağlık kapısı ölçütleri sıfır olamaz")
	}
	return &Rollout{lifecycle: l, store: a, rec: rec, clock: realClock{}, gate: gate}, nil
}

// Run, sürümü ayağa kaldırır, kapıdan geçirir ve trafiği devreder.
//
// ── Sıra taşıyıcıdır ────────────────────────────────────────────────
//
//	ağ → konteynerler → başlat → KAPI → SetActiveRelease → uzlaştır
//
// `SetActiveRelease` kapıdan SONRA: kontrol düzlemine "bu sürüm canlı"
// yazmak, ters vekile yüklemekten önce gelmeli (aksi hâlde arada düşen
// bir panelyd, canlıda kayıtsız bir rota bırakır ve bir sonraki
// uzlaştırma onu sessizce siler) — ama ikisi de kapıdan sonra gelmeli.
//
// ⚠ ESKİ SÜRÜM DURDURULMUYOR. Bu dilimde boşaltma ve geri alma yok;
// eski konteynerler ayakta kalıyor ama uzlaştırıcı YALNIZCA aktif sürümün
// replikalarını rotalıyor, yani trafik almıyorlar. Kaynak tüketiyorlar ve
// bu bilinen bir eksik.
func (r *Rollout) Run(ctx context.Context, app store.App, rel store.Release) error {
	if _, err := r.lifecycle.EnsureNetwork(ctx, app.ID); err != nil {
		return err
	}

	for i := uint32(0); i < app.Replicas; i++ {
		if err := r.lifecycle.CreateReplica(ctx, execclient.CreateReplicaOptions{
			AppID:         app.ID,
			ReleaseID:     rel.ID,
			Index:         i,
			CommitSHA:     rel.CommitSHA,
			ContainerPort: app.ContainerPort,
			Limits: execclient.Limits{
				MemoryBytes: app.MemoryBytes,
				CPUMillis:   app.CPUMillis,
				BlkioWeight: app.BlkioWeight,
			},
		}); err != nil {
			return err
		}
		if err := r.lifecycle.StartReplica(ctx, app.ID, rel.ID, i); err != nil {
			return err
		}
	}

	if err := r.awaitReady(ctx, app, rel); err != nil {
		// Trafiğe DOKUNULMADI: eski sürüm hâlâ canlı ve öyle kalıyor.
		return fmt.Errorf("dağıtım sağlık kapısında durdu, TRAFİK TAŞINMADI: %w", err)
	}

	if err := r.store.SetActiveRelease(ctx, app.ID, rel.ID); err != nil {
		return err
	}

	res, err := r.rec.Reconcile(ctx)
	if err != nil {
		return err
	}
	// Atlanan uygulamalar bir hata DEĞİL ama sessiz de olmamalı: çağıran
	// bunu günlüğe yazıyor.
	if len(res.Skipped) > 0 {
		return SkippedError{Result: res}
	}
	return nil
}

// SkippedError, yükleme başarılı olduğu hâlde bazı uygulamaların
// rotalanamadığını bildirir.
//
// ── Neden ayrı bir tip? ─────────────────────────────────────────────
//
// Sonucu `(Result, error)` çiftiyle döndürmek, `if err != nil` yazan her
// çağıranın "sitelerin yarısı düştü"yü BAŞARI sayması demekti. Hata
// arayüzünü uygulayan ayrı bir tip, durumu görmezden gelmeyi zorlaştırır:
// yutmak için açıkça `errors.As` ile ayıklamak gerekir.
type SkippedError struct{ Result Result }

func (e SkippedError) Error() string {
	return "bazı uygulamalar rotalanamadı — " + e.Result.Error()
}

// awaitReady, replikaların trafiğe HAZIR olmasını bekler.
//
// ══════════════════════════════════════════════════════════════════════
//
//	⚠ BU BİR HTTP SAĞLIK YOKLAMASI DEĞİLDİR
//
// ══════════════════════════════════════════════════════════════════════
//
// Ölçtüğü şey: konteyner RUNNING mı ve uygulama ağında bir adresi var mı.
// Ölçmediği şey: uygulamanın gerçekten cevap verip vermediği.
//
// HTTP yoklaması panelyd'nin ağa çıkmasını gerektiriyor; birimi
// `RestrictAddressFamilies=AF_UNIX` ile çalışıyor ve bunu gevşetmek ayrı
// bir karar (panelyd.service içindeki not bu değişikliği zaten öngörüyor).
// O yüzden burada YOK ve olmadığı saklanmıyor.
//
// Yine de boş bir kapı değil: çöken bir commit'in en yaygın belirtisi
// konteynerin açılışta ölmesidir ve o konteyner RUNNING olmaz — EXITED
// olur, kapıdan geçemez, trafik taşınmaz.
//
// ── Ölçüt POZİTİF ──────────────────────────────────────────────────
//
// "Hata görmedim" değil, "arka arkaya N ölçümde HAZIR gördüm". Fark
// önemli: açılışta bir süre RUNNING görünüp sonra ölen bir konteyner tek
// bir ölçümü geçerdi. Ardışık sayaç, ilk başarısız ölçümde SIFIRLANIYOR.
func (r *Rollout) awaitReady(ctx context.Context, app store.App, rel store.Release) error {
	deadline := r.clock.Now().Add(r.gate.Timeout)
	streak := 0
	var last string

	for {
		ready, why := r.readyCount(ctx, app.ID, rel.ID)
		if ready >= app.Replicas {
			streak++
			if streak >= r.gate.Successes {
				return nil
			}
		} else {
			// SIFIRLANIYOR: aralıklı bir başarı, sağlıklı sayılmaz.
			streak = 0
			last = fmt.Sprintf("%d/%d replika hazır (%s)", ready, app.Replicas, why)
		}

		if !r.clock.Now().Before(deadline) {
			if last == "" {
				last = fmt.Sprintf("yalnızca %d ardışık başarılı ölçüm (%d gerekli)",
					streak, r.gate.Successes)
			}
			return fmt.Errorf("süre doldu: %s", last)
		}
		if err := r.clock.Sleep(ctx, r.gate.Interval); err != nil {
			return fmt.Errorf("bekleme kesildi: %w", err)
		}
	}
}

// readyCount, sürümün trafiğe hazır replika sayısını döndürür.
func (r *Rollout) readyCount(ctx context.Context, appID, releaseID string) (uint32, string) {
	reps, err := r.lifecycle.ListReplicas(ctx, appID)
	if err != nil {
		return 0, fmt.Sprintf("liste alınamadı: %v", err)
	}

	var ready uint32
	states := map[string]int{}
	for _, rep := range reps {
		if rep.ReleaseID != releaseID {
			continue
		}
		if rep.Routable() {
			ready++
			continue
		}
		states[rep.State.String()]++
	}
	if len(states) == 0 {
		return ready, "bu sürümün konteyneri görünmüyor"
	}
	return ready, fmt.Sprintf("durumlar: %v", states)
}

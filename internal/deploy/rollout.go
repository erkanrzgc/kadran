package deploy

import (
	"context"
	"errors"
	"fmt"
	"sort"
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
	StopRelease(ctx context.Context, appID, releaseID string, grace time.Duration) (uint32, error)
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

	gate  GateOptions
	drain DrainOptions
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

// DrainOptions, trafiğin devredilmesinden SONRAKİ kapanış ölçütüdür.
type DrainOptions struct {
	// Window, ters vekil yeni sürüme çevrildikten sonra eski
	// konteynerleri durdurmadan önce beklenen süre.
	//
	// Sıfır olamaz: Caddy yeni upstream'lere çevrilse bile eski
	// konteynerlere UÇAN istekler olabilir ve onları anında öldürmek,
	// dağıtım anında bir avuç kullanıcıya kopmuş bağlantı göstermek
	// demektir.
	Window time.Duration
	// Grace, konteynere SIGTERM ile SIGKILL arasında verilen süre.
	Grace time.Duration
}

// DefaultDrain, makul varsayılanlar.
var DefaultDrain = DrainOptions{Window: 10 * time.Second, Grace: 10 * time.Second}

// NewRollout, dağıtım orkestratörünü kurar.
func NewRollout(
	l Lifecycle, a Activations, rec *Reconciler, gate GateOptions, drain DrainOptions,
) (*Rollout, error) {
	if l == nil || a == nil || rec == nil {
		return nil, errors.New("deploy: yaşam döngüsü, aktivasyon ve uzlaştırıcı zorunlu")
	}
	if gate.Successes <= 0 || gate.Interval <= 0 || gate.Timeout <= 0 {
		return nil, errors.New("deploy: sağlık kapısı ölçütleri sıfır olamaz")
	}
	if drain.Window <= 0 || drain.Grace <= 0 {
		return nil, errors.New("deploy: boşaltma penceresi ve kapanış süresi sıfır olamaz")
	}
	return &Rollout{
		lifecycle: l, store: a, rec: rec, clock: realClock{},
		gate: gate, drain: drain,
	}, nil
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
// Uzlaştırmadan sonra boşaltma penceresi beklenir ve ESKİ sürümlerin
// konteynerleri durdurulur. Durdurma yalnızca trafiğin GERÇEKTEN taşındığı
// doğrulandıktan sonra yapılır — ayrıntı `drainStale`'de.
//
// ⚠ Durduruluyor ama SİLİNMİYOR: geri alma, duran konteyneri yeniden
// başlatarak saniyeler içinde çalışsın diye. Silme politikası (son N
// sürümü tut) dağıtım geçmişiyle birlikte gelecek.
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

	// ⚠ BİZİM uygulamamız atlandıysa trafik TAŞINMADI: ters vekilde hâlâ
	// eski sürümün rotası duruyor (ya da hiç rota yok). Eski konteynerleri
	// burada durdurmak, siteyi DÜŞÜRÜRDÜ. Boşaltmaya hiç girilmiyor.
	if _, ourAppSkipped := res.Skipped[app.ID]; ourAppSkipped {
		return SkippedError{Result: res}
	}

	drainErr := r.drainStale(ctx, app.ID, rel.ID)

	// Atlanan uygulamalar bir hata DEĞİL ama sessiz de olmamalı: çağıran
	// bunu günlüğe yazıyor. İki durum aynı anda olabileceği için
	// birleştiriliyor — biri diğerini örtmemeli.
	var skippedErr error
	if len(res.Skipped) > 0 {
		skippedErr = SkippedError{Result: res}
	}
	return errors.Join(skippedErr, drainErr)
}

// DrainError, dağıtımın BAŞARILI olduğunu ama eski sürümün
// durdurulamadığını bildirir.
//
// ── Neden dağıtım başarısızlığı değil? ──────────────────────────────
//
// Trafik bu noktada çoktan yeni sürüme taşındı ve site sağlıklı. Durdurma
// hatası bir KAYNAK SIZINTISIDIR, bir kesinti değil. `Run`'dan düz bir
// hata döndürmek, CLI'ın çalışan bir dağıtıma "başarısız" demesine yol
// açardı — kullanıcı da muhtemelen geri alırdı. Ayrı tip, çağıranın farkı
// görmesini sağlıyor.
type DrainError struct {
	AppID string
	Err   error
}

func (e DrainError) Error() string {
	return fmt.Sprintf(
		"dağıtım başarılı (trafik taşındı) ama eski sürüm durdurulamadı — %s: %v",
		e.AppID, e.Err)
}

func (e DrainError) Unwrap() error { return e.Err }

// drainStale, trafik taşındıktan sonra eski sürümleri kapatır.
//
// ── Sıra taşıyıcıdır ────────────────────────────────────────────────
//
//	Caddy yüklendi → BOŞALTMA PENCERESİ → durdur
//
// Pencere olmadan: ters vekil yeni upstream'lere çevrilmiş olsa bile eski
// konteynerlere UÇAN istekler var. Onları anında öldürmek, her dağıtımda
// bir avuç kullanıcıya yarım kalmış yanıt göstermek demek.
//
// Pencereden önce durdurmak daha da kötü olurdu: o an trafiği ALAN
// konteyner ölürdü.
func (r *Rollout) drainStale(ctx context.Context, appID, activeID string) error {
	stale, err := r.staleReleases(ctx, appID, activeID)
	if err != nil {
		return DrainError{AppID: appID, Err: err}
	}
	if len(stale) == 0 {
		return nil
	}

	if err := r.clock.Sleep(ctx, r.drain.Window); err != nil {
		return DrainError{AppID: appID, Err: fmt.Errorf("boşaltma kesildi: %w", err)}
	}

	// Tek bir sürümün durdurulamaması diğerlerini engellemiyor: amaç
	// mümkün olduğunca çok kaynağı geri almak, ilk hatada kaçmak değil.
	var errs []error
	for _, id := range stale {
		if _, err := r.lifecycle.StopRelease(ctx, appID, id, r.drain.Grace); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
		}
	}
	if len(errs) > 0 {
		return DrainError{AppID: appID, Err: errors.Join(errs...)}
	}
	return nil
}

// staleReleases, hostta duran ama artık AKTİF OLMAYAN sürümleri döndürür.
//
// Kaynak, kontrol düzlemi değil HOST: SQLite ne istediğimizi biliyor, ama
// yalnızca Docker hangi konteynerlerin gerçekten var olduğunu biliyor.
// Önceki bir panelyd çökmesinden kalan sürümler de böylece toplanıyor.
//
// Sıra BELİRLENİMLİ: aksi hâlde aynı arıza her koşuda başka bir sırayla
// günlüğe düşer ve karşılaştırılamaz.
func (r *Rollout) staleReleases(ctx context.Context, appID, activeID string) ([]string, error) {
	reps, err := r.lifecycle.ListReplicas(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("konteynerler listelenemedi: %w", err)
	}

	seen := map[string]struct{}{}
	var out []string
	for _, rep := range reps {
		if rep.ReleaseID == activeID || rep.ReleaseID == "" {
			continue
		}
		if _, dup := seen[rep.ReleaseID]; dup {
			continue
		}
		seen[rep.ReleaseID] = struct{}{}
		out = append(out, rep.ReleaseID)
	}
	sort.Strings(out)
	return out, nil
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

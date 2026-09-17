// Package health, çöken uygulamaları kimse istemeden ayağa kaldıran
// gözetmen döngüsünü içerir.
//
// ── Daemon'ın var olma sebebi ───────────────────────────────────────
//
// Panely'nin geri kalanı istek-cevap: bir komut gelir, bir şey olur,
// cevap döner. Bu paket farklı — istemci bağlı olmasa da, kimse
// bakmıyorken de çalışır. Faz 1 ölçütü #3 tam olarak bunu ölçüyor:
// `docker kill` sonrası uygulama 30 saniye içinde geri gelmeli.
package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/erkanrzgc/panely/internal/alarm"
	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/store"
)

// Healer, sağlık ölçümünü ve iyileştirmeyi yapan orkestratördür.
//
// İkisi de `*deploy.Rollout` üzerinde: "sağlıklı" tanımı dağıtım
// kapısının kullandığı tanımın AYNISI olsun diye. İki kopya olsaydı biri
// düzeltilirken diğeri kayardı ve Faz 1'in iki ölçütü birbirinden
// habersiz farklı şeyler ölçmeye başlardı.
type Healer interface {
	Check(ctx context.Context, app store.App, releaseID string) (uint32, string)
	Heal(ctx context.Context, app store.App, rel store.Release) (bool, error)
}

// Deployments, gözetmenin neyi izleyeceğini söyleyen kontrol düzlemidir.
type Deployments interface {
	ActiveDeployments(ctx context.Context) ([]store.Deployment, error)
	ActiveDeployment(ctx context.Context, appID string) (store.Deployment, error)
	GetApp(ctx context.Context, id string) (store.App, error)
	GetRelease(ctx context.Context, appID, releaseID string) (store.Release, error)
}

// Auditor, gözetmenin kararlarını hash-zincirine yazar.
type Auditor interface {
	AppendAudit(ctx context.Context, rec audit.Record) (audit.Record, error)
}

// Alarms, gözetmenin "artık kurtaramıyorum" noktasını dışarı bildirir.
//
// ── Neden gözetmen bunu kendisi karar vermiyor ──────────────────────
//
// Gözetmen iyileştirmeyi SONSUZA KADAR deniyor ve bu doğru: geçici bir
// arıza (dolu disk, düşen bağımlılık) sonradan düzelebilir ve o an
// uygulama kendiliğinden ayağa kalkmalı. Ama denemeyi sürdürmek ile
// "her şey yolunda" demek aynı şey değil — art arda başarısız
// iyileştirme, operatörün bilmesi gereken bir durumdur.
//
// Alarm mantığı burada DEĞİL çünkü kenar tetikleme durumu panelyd'nin
// ömründen uzun yaşamak zorunda; gözetmenin kendi durumu ise bilerek
// bellekte ve süreçle birlikte ölüyor.
type Alarms interface {
	Raise(ctx context.Context, a store.Alarm)
	Clear(ctx context.Context, id string)
}

// NopAlarms, hiçbir şey bildirmeyen alarm hedefidir.
//
// Yalnızca alarm davranışını sınamayan testler için. Üretimde
// kullanılması, sessiz arızaya karşı yazılmış mekanizmanın sessizce
// kapatılması olurdu.
type NopAlarms struct{}

// Raise, hiçbir şey yapmaz.
func (NopAlarms) Raise(context.Context, store.Alarm) {}

// Clear, hiçbir şey yapmaz.
func (NopAlarms) Clear(context.Context, string) {}

// Clock, bekleme davranışını sınanabilir kılar.
//
// ZORUNLU, isteğe bağlı değil: gerçek saatle yazılmış bir gözetmen testi
// 30 saniyelik uykular demektir ve tam da kararlılığın en kritik olduğu
// yerde titrek testler üretir.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock, gerçek zamanı kullanan saattir.
//
// `deploy` paketindekinin kopyası değil ikizi: on satırlık bir sarmalayıcı
// için paketler arası bağımlılık kurmak, "sağlıklı" tanımını paylaşmak
// gibi anlamlı bir paylaşım değil. Bu tip bir gün değişirse ikisinin
// birlikte değişmesi de gerekmiyor.
func SystemClock() Clock { return systemClock{} }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Options, gözetmenin davranışını belirler.
type Options struct {
	// Interval, ölçümler arası bekleme.
	Interval time.Duration
	// FailuresBeforeHeal, iyileştirmeden önce kaç ARDIŞIK başarısız
	// ölçüm gerektiği.
	FailuresBeforeHeal int
	// BackoffBase, ilk iyileştirmeden sonraki bekleme.
	BackoffBase time.Duration
	// BackoffMax, geri çekilmenin üst sınırı.
	BackoffMax time.Duration
	// HealsBeforeAlarm, alarm açılmadan önce kaç iyileştirmenin ARDIŞIK
	// başarısız olması gerektiği.
	HealsBeforeAlarm int
}

// DefaultOptions, Faz 1 ölçütü #3'ün bütçesine göre seçildi.
//
//	tespit     : 3 × 2sn                    ≈  6 sn
//	iyileştirme: deploy.DefaultHealGate     ≈  4 sn
//	                                  toplam ≈ 10 sn   (bütçe: 30 sn)
//
// `FailuresBeforeHeal: 1` OLMAZ: tek bir başarısız yoklama, ağır yük
// altındaki sağlıklı bir uygulamada da olur ve gözetmen onu yeniden
// başlatarak GERÇEK bir kesinti üretirdi. Üç ardışık ölçüm, geçici
// dalgalanmayı çökmeden ayırıyor.
//
// Bu değerleri büyütmeden önce yukarıdaki toplamı yeniden hesapla —
// testler sahte saatle koştuğu için ölçüt kaçırıldığında YEŞİL kalırlar.
var DefaultOptions = Options{
	Interval:           2 * time.Second,
	FailuresBeforeHeal: 3,
	BackoffBase:        15 * time.Second,
	BackoffMax:         10 * time.Minute,
	// Üç ardışık başarısız iyileştirme ≈ 15+30+60 = 105 saniye.
	//
	// `1` OLMAZ: tek bir başarısız iyileştirme, imaj çekilirken ya da
	// host anlık yük altındayken de olur ve alarm hemen çalarsa
	// kendiliğinden düzelen durumlar için gürültü üretir — yani alarmın
	// güvenilirliğini ilk günden yakar.
	HealsBeforeAlarm: 3,
}

// appState, tek bir uygulamanın gözetim durumudur.
//
// ⚠ BELLEKTE tutuluyor, diske yazılmıyor: panelyd yeniden başlayınca
// sayaçlar sıfırlanır ve sürekli çöken bir uygulama geri çekilmesini
// kaybeder. Kabul edilen bir ödünç — kalıcılaştırmak her yoklamada yazma
// demekti ve gözetmenin kendisi bir sorun kaynağına dönüşürdü.
type appState struct {
	releaseID  string
	failures   int
	unhealthy  bool
	heals      int
	nextHealAt time.Time
}

// Supervisor, aktif dağıtımları yoklar ve çökenleri ayağa kaldırır.
type Supervisor struct {
	healer Healer
	store  Deployments
	audit  Auditor
	alarms Alarms
	clock  Clock
	opts   Options

	// state, uygulama kimliğinden duruma. Yalnızca Run/Cycle'ın
	// goroutine'inden okunuyor ve yazılıyor.
	state map[string]*appState
}

// New, gözetmeni kurar.
func New(
	h Healer, d Deployments, a Auditor, al Alarms, c Clock, o Options,
) (*Supervisor, error) {
	if h == nil || d == nil || a == nil || al == nil || c == nil {
		return nil, errors.New(
			"health: iyileştirici, depo, denetçi, alarm ve saat zorunlu")
	}
	if o.Interval <= 0 || o.FailuresBeforeHeal <= 0 {
		return nil, errors.New("health: aralık ve başarısızlık eşiği sıfır olamaz")
	}
	if o.BackoffBase <= 0 || o.BackoffMax < o.BackoffBase {
		return nil, errors.New("health: geri çekilme tabanı sıfır olamaz ve tavanı aşamaz")
	}
	if o.HealsBeforeAlarm <= 0 {
		return nil, errors.New("health: alarm eşiği sıfır olamaz")
	}
	return &Supervisor{
		healer: h, store: d, audit: a, alarms: al, clock: c, opts: o,
		state: map[string]*appState{},
	}, nil
}

// Run, bağlam iptal edilene kadar döngüyü sürdürür.
//
// Önce BEKLİYOR, sonra ölçüyor: panelyd açılışta zaten uzlaştırma
// yapıyor ve konteynerler o sırada ayağa kalkıyor. Hemen ölçseydik,
// açılışın normal geçiş anını "çökmüş" diye okuyup gereksiz bir
// iyileştirme tetiklerdik.
func (s *Supervisor) Run(ctx context.Context) error {
	slog.Info("sağlık gözetmeni başladı",
		"aralik", s.opts.Interval,
		"esik", s.opts.FailuresBeforeHeal)

	for {
		if err := s.clock.Sleep(ctx, s.opts.Interval); err != nil {
			slog.Info("sağlık gözetmeni durdu")
			return ctx.Err()
		}
		s.Cycle(ctx)
	}
}

// Cycle, tek bir gözetim turudur.
//
// Dışa açık: testler döngüyü gerçek zamanla koşturmak zorunda kalmasın.
// Hiçbir hata döndürmüyor — tek bir uygulamanın sorunu diğerlerinin
// gözetimini durdurmamalı; hatalar günlüğe ve denetim zincirine gidiyor.
func (s *Supervisor) Cycle(ctx context.Context) {
	deps, err := s.store.ActiveDeployments(ctx)
	if err != nil {
		slog.Warn("gözetim: aktif dağıtımlar okunamadı", "hata", err)
		return
	}

	live := make(map[string]struct{}, len(deps))
	for _, d := range deps {
		live[d.AppID] = struct{}{}
		s.visit(ctx, d)
	}

	// Artık aktif olmayan uygulamaların durumu bırakılıyor: silinen ya da
	// dağıtımı kaldırılan uygulamalar için sayaç tutmak, haritayı
	// sınırsız büyütürdü.
	for id := range s.state {
		if _, ok := live[id]; !ok {
			delete(s.state, id)
		}
	}
}

// visit, tek bir dağıtımı ölçer ve gerekiyorsa iyileştirir.
func (s *Supervisor) visit(ctx context.Context, d store.Deployment) {
	st := s.stateFor(d)

	app, err := s.store.GetApp(ctx, d.AppID)
	if err != nil {
		slog.Warn("gözetim: uygulama okunamadı", "uygulama", d.AppID, "hata", err)
		return
	}

	ready, why := s.healer.Check(ctx, app, d.ReleaseID)
	if ready >= app.Replicas {
		s.markHealthy(ctx, d.AppID, st)
		return
	}

	st.failures++
	if st.failures < s.opts.FailuresBeforeHeal {
		return
	}
	if !st.unhealthy {
		st.unhealthy = true
		slog.Warn("uygulama sağlıksız", "uygulama", d.AppID, "sebep", why)
		s.record(ctx, "app.unhealthy", d, audit.OutcomeFailure,
			fmt.Sprintf(`{"ready":%d,"replicas":%d,"why":%q}`, ready, app.Replicas, why))
	}
	if s.clock.Now().Before(st.nextHealAt) {
		return
	}
	s.heal(ctx, app, d, st)
}

// stateFor, uygulamanın durumunu döndürür; sürüm değiştiyse SIFIRLAR.
//
// Yeni bir dağıtım ya da geri alma, eski sürümün sayaçlarını geçersiz
// kılar: taze sürüm, öncekinin geri çekilme cezasını miras almamalı.
func (s *Supervisor) stateFor(d store.Deployment) *appState {
	st, ok := s.state[d.AppID]
	if !ok || st.releaseID != d.ReleaseID {
		st = &appState{releaseID: d.ReleaseID}
		s.state[d.AppID] = st
	}
	return st
}

// markHealthy, sağlıklı ölçümü işler ve GEÇİŞ anında denetim kaydı yazar.
//
// Her sağlıklı turda değil yalnızca geçişte: iki saniyede bir kayıt,
// hash zincirini gürültüyle doldurup `audit verify` çıktısını
// okunamaz hâle getirirdi.
func (s *Supervisor) markHealthy(ctx context.Context, appID string, st *appState) {
	if st.unhealthy {
		slog.Info("uygulama iyileşti", "uygulama", appID, "deneme", st.heals)
		s.record(ctx, "app.healed",
			store.Deployment{AppID: appID, ReleaseID: st.releaseID},
			audit.OutcomeSuccess, fmt.Sprintf(`{"attempts":%d}`, st.heals))
	}
	st.failures = 0
	st.unhealthy = false
	st.heals = 0
	st.nextHealAt = time.Time{}

	// Alarm KOŞULSUZ kapatılıyor — `st.unhealthy` kontrolünün dışında.
	//
	// Gerekçe yeniden başlatma: panelyd çöküp kalktığında bellekteki
	// `st` sıfırlanır ama alarm satırı diskte DURUR. Kapatmayı
	// `st.unhealthy`'ye bağlasaydık, yeniden başlatmadan sonra sağlıklı
	// dönen bir uygulamanın alarmı sonsuza kadar açık kalırdı — ve
	// kapanmayan bir alarm, bakılmayan bir alarma dönüşür.
	//
	// Kapatma da kenar tetiklemeli: etkin alarm yoksa sessiz.
	s.alarms.Clear(ctx, alarm.KindHealExhausted+":"+appID)
}

// heal, iyileştirmeyi dener ve geri çekilmeyi ilerletir.
func (s *Supervisor) heal(ctx context.Context, app store.App, d store.Deployment, st *appState) {
	// ── YARIŞ KORUMASI ──────────────────────────────────────────────
	//
	// Aktif sürüm, tur başında okunduktan sonra değişmiş olabilir: aynı
	// anda bir dağıtım ya da geri alma trafiği taşımış olabilir. Bunu
	// kontrol etmeseydik, TAM O SIRADA eskiyen bir sürümün konteynerlerini
	// ayağa kaldırır ve boşaltmayla yarışırdık.
	cur, err := s.store.ActiveDeployment(ctx, d.AppID)
	if err != nil || cur.ReleaseID != d.ReleaseID {
		slog.Info("gözetim: aktif sürüm değişti, iyileştirme atlandı",
			"uygulama", d.AppID, "beklenen", d.ReleaseID, "guncel", cur.ReleaseID)
		delete(s.state, d.AppID)
		return
	}

	rel, err := s.store.GetRelease(ctx, d.AppID, d.ReleaseID)
	if err != nil {
		slog.Warn("gözetim: sürüm okunamadı", "uygulama", d.AppID, "hata", err)
		return
	}

	st.heals++
	st.nextHealAt = s.clock.Now().Add(s.backoff(st.heals))
	// Sayaç sıfırlanıyor: iyileştirmeden sonra konteynerin açılması için
	// zaman tanınmalı. Sıfırlamasaydık bir sonraki tur eşiği ANINDA
	// aşar ve geri çekilme hiç devreye girmeden art arda denerdik.
	st.failures = 0

	recreated, healErr := s.healer.Heal(ctx, app, rel)
	if healErr != nil {
		slog.Warn("iyileştirme başarısız", "uygulama", d.AppID,
			"deneme", st.heals, "sonraki", st.nextHealAt, "hata", healErr)
		s.record(ctx, "app.heal", d, audit.OutcomeFailure,
			fmt.Sprintf(`{"attempt":%d,"error":%q}`, st.heals, healErr.Error()))

		// ── "Kurtaramazsa haberin olmaz" ────────────────────────────
		//
		// Gözetmen denemeyi sürdürüyor ve sürdürmeli; ama eşiği aşan
		// ardışık başarısızlık artık geçici bir dalgalanma değil.
		//
		// Her başarısızlıkta değil, EŞİKTEN SONRA her turda çağrılıyor:
		// alarmın kendisi kenar tetiklemeli olduğu için tekrarlar
		// sessizce yutuluyor ve burada ayrıca sayaç tutmaya gerek yok.
		if st.heals >= s.opts.HealsBeforeAlarm {
			s.alarms.Raise(ctx, store.Alarm{
				ID:       alarm.KindHealExhausted + ":" + d.AppID,
				Kind:     alarm.KindHealExhausted,
				Target:   d.AppID,
				Severity: store.SeverityCritical,
				Since:    s.clock.Now(),
				// ⚠ healErr'in METNİ yazılmıyor: alarm ayrıntısı
				// denetim zincirine de düşüyor ve zincir ekle-sadece.
				// Derleme/çalıştırma hatası kullanıcının deposundan
				// gelen metin taşıyabilir (K-053'ün aynı gerekçesi).
				Detail: fmt.Sprintf(
					"%d ardışık iyileştirme başarısız — gözetmen "+
						"uygulamayı ayağa kaldıramıyor", st.heals),
			})
		}
		return
	}
	slog.Info("iyileştirme uygulandı", "uygulama", d.AppID,
		"deneme", st.heals, "yeniden_kuruldu", recreated)
	s.record(ctx, "app.heal", d, audit.OutcomeSuccess,
		fmt.Sprintf(`{"attempt":%d,"recreated":%t}`, st.heals, recreated))
}

// backoff, n'inci denemeden sonraki beklemeyi verir.
//
// Üstel ve TAVANLI: sürekli çöken bir uygulama (bozuk imaj, eksik ortam
// değişkeni) sonsuza kadar saniyede bir yeniden başlatılırsa hem
// günlükleri hem CPU'yu yer. Tavan olmasaydı kayma sonunda taşardı.
func (s *Supervisor) backoff(n int) time.Duration {
	d := s.opts.BackoffBase
	for i := 1; i < n; i++ {
		d *= 2
		if d >= s.opts.BackoffMax {
			return s.opts.BackoffMax
		}
	}
	return d
}

// record, denetim zincirine yazar. Yazamamak gözetimi durdurmaz.
func (s *Supervisor) record(
	ctx context.Context, action string, d store.Deployment,
	outcome audit.Outcome, params string,
) {
	rec := audit.Record{
		Actor:      audit.SystemActor("supervisor"),
		Action:     action,
		Target:     "app/" + d.AppID + "/release/" + d.ReleaseID,
		ParamsJSON: params,
		Outcome:    outcome,
		Source:     audit.SourceDaemon,
	}
	if _, err := s.audit.AppendAudit(ctx, rec); err != nil {
		slog.Error("gözetim denetim kaydı yazılamadı", "eylem", action, "hata", err)
	}
}

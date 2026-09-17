package health

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/audit"
	"github.com/erkanrzgc/panely/internal/store"
)

const (
	testApp = "portfolio"
	testRel = "r3"
)

// ── Sahteler ─────────────────────────────────────────────────────────

// fakeHealer, sağlık durumunu DURUM OLARAK tutar.
//
// `Heal` gerçekten `healthy`'yi çeviriyor — sabit bir cevap döndüren bir
// sahte, iyileştirmenin hiçbir işe yaramadığı bir gözetmeni de başarılı
// gösterirdi. "İyileşti" iddiası ancak ölçüm DEĞİŞTİĞİNDE anlamlı.
type fakeHealer struct {
	healthy bool
	// healFixes, iyileştirmenin işe yarayıp yaramadığını belirler.
	healFixes bool
	healErr   error

	heals int
}

func (f *fakeHealer) Check(context.Context, store.App, string) (uint32, string) {
	if f.healthy {
		return 1, ""
	}
	return 0, "konteyner cevap vermiyor"
}

func (f *fakeHealer) Heal(context.Context, store.App, store.Release) (bool, error) {
	f.heals++
	if f.healErr != nil {
		return false, f.healErr
	}
	if f.healFixes {
		f.healthy = true
	}
	return false, nil
}

type fakeStore struct {
	deps       []store.Deployment
	active     store.Deployment
	activeErr  error
	appErr     error
	releaseErr error
}

func (f *fakeStore) ActiveDeployments(context.Context) ([]store.Deployment, error) {
	return f.deps, nil
}

func (f *fakeStore) ActiveDeployment(_ context.Context, appID string) (store.Deployment, error) {
	if f.activeErr != nil {
		return store.Deployment{}, f.activeErr
	}
	if f.active.AppID != "" {
		return f.active, nil
	}
	for _, d := range f.deps {
		if d.AppID == appID {
			return d, nil
		}
	}
	return store.Deployment{}, errors.New("yok")
}

func (f *fakeStore) GetApp(_ context.Context, id string) (store.App, error) {
	if f.appErr != nil {
		return store.App{}, f.appErr
	}
	return store.App{ID: id, Replicas: 1, ContainerPort: 8080, HealthPath: "/"}, nil
}

func (f *fakeStore) GetRelease(_ context.Context, app, rel string) (store.Release, error) {
	if f.releaseErr != nil {
		return store.Release{}, f.releaseErr
	}
	return store.Release{AppID: app, ID: rel, CommitSHA: strings.Repeat("c", 40)}, nil
}

type fakeAuditor struct{ records []audit.Record }

func (f *fakeAuditor) AppendAudit(_ context.Context, r audit.Record) (audit.Record, error) {
	f.records = append(f.records, r)
	return r, nil
}

func (f *fakeAuditor) actions() []string {
	out := make([]string, 0, len(f.records))
	for _, r := range f.records {
		out = append(out, r.Action)
	}
	return out
}

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func (c *fakeClock) Sleep(_ context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return nil
}

// fakeAlarms, yükseltilen ve kapatılan alarmları kaydeder.
//
// NopAlarms KULLANILMIYOR: gözetmenin "artık kurtaramıyorum" bildirimi
// bu dilimin iddialarından biri ve hiçbir şey yapmayan bir hedefle
// sınanamaz.
type fakeAlarms struct {
	raised  []store.Alarm
	cleared []string
}

func (f *fakeAlarms) Raise(_ context.Context, a store.Alarm) {
	f.raised = append(f.raised, a)
}

func (f *fakeAlarms) Clear(_ context.Context, id string) {
	f.cleared = append(f.cleared, id)
}

type harness struct {
	sup    *Supervisor
	healer *fakeHealer
	store  *fakeStore
	audit  *fakeAuditor
	alarms *fakeAlarms
	clock  *fakeClock
}

func newHarness(t *testing.T, healer *fakeHealer) *harness {
	t.Helper()
	return newHarnessWith(t, healer, DefaultOptions)
}

func newHarnessWith(t *testing.T, healer *fakeHealer, opts Options) *harness {
	t.Helper()
	st := &fakeStore{deps: []store.Deployment{{
		AppID: testApp, ReleaseID: testRel, Domain: "example.test", ContainerPort: 8080,
	}}}
	au := &fakeAuditor{}
	al := &fakeAlarms{}
	cl := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	sup, err := New(healer, st, au, al, cl, opts)
	if err != nil {
		t.Fatalf("gözetmen kurulamadı: %v", err)
	}
	return &harness{
		sup: sup, healer: healer, store: st, audit: au, alarms: al, clock: cl,
	}
}

// cycles, n tur koşturur.
func (h *harness) cycles(n int) {
	for i := 0; i < n; i++ {
		h.sup.Cycle(context.Background())
	}
}

// ── Testler ──────────────────────────────────────────────────────────

// TestHealthyAppIsNeverHealed, sağlıklı bir uygulamaya DOKUNULMADIĞINI
// doğrular.
//
// Gözetmenin en kolay yoldan zarar verme biçimi budur: çalışan bir
// uygulamayı gereksiz yere yeniden başlatmak, olmayan bir sorunu gerçek
// bir kesintiye çevirir.
func TestHealthyAppIsNeverHealed(t *testing.T) {
	h := newHarness(t, &fakeHealer{healthy: true})
	h.cycles(10)

	if h.healer.heals != 0 {
		t.Errorf("sağlıklı uygulama %d kez iyileştirildi", h.healer.heals)
	}
	if len(h.audit.records) != 0 {
		t.Errorf("sağlıklı uygulama için denetim kaydı yazıldı: %v", h.audit.actions())
	}
}

// TestHealWaitsForConsecutiveFailures, eşiğin ALTINDA iyileştirme
// yapılmadığını doğrular.
//
// Tek bir başarısız yoklama ağır yük altında da olur. Eşik, geçici
// dalgalanmayı çökmeden ayıran şey.
//
// ── Beklenti SABİTTEN okunmuyor, ve bu kasıtlı ──────────────────────
//
// İlk hâli `DefaultOptions.FailuresBeforeHeal - 1` tur koşuyordu. Kendi
// kendine referans veren bir iddiaydı: eşik 1'e düşürüldüğünde testin
// beklentisi de onunla birlikte kaydı ve mutasyon GÖRÜNMEZ kaldı
// (mutasyon sınamasında yakalandı). Sayılar artık düz yazılı.
func TestHealWaitsForConsecutiveFailures(t *testing.T) {
	opts := DefaultOptions
	opts.FailuresBeforeHeal = 3
	h := newHarnessWith(t, &fakeHealer{healthy: false}, opts)

	h.cycles(2) // eşiğin bir altı
	if h.healer.heals != 0 {
		t.Fatalf("iki başarısız ölçümde iyileştirildi — eşik 3 olmalıydı")
	}

	h.cycles(1) // üçüncü ardışık başarısızlık
	if h.healer.heals != 1 {
		t.Errorf("üçüncü başarısızlıkta iyileştirme %d, 1 bekleniyordu", h.healer.heals)
	}
}

// TestDefaultThresholdIsNotOne, VARSAYILAN eşiğin tek bir dalgalanmaya
// müdahale etmeyecek kadar yüksek olduğunu doğrular.
//
// Yukarıdaki test mekanizmayı sınıyor, bu ise seçilen sayıyı. İkisi ayrı
// olmasaydı, `FailuresBeforeHeal: 1` sessizce geçerdi — mekanizma yine
// doğru çalışır, ama gözetmen sağlıklı bir uygulamayı ilk kekemelikte
// yeniden başlatıp GERÇEK bir kesinti üretirdi.
func TestDefaultThresholdIsNotOne(t *testing.T) {
	if DefaultOptions.FailuresBeforeHeal < 2 {
		t.Errorf("varsayılan eşik %d — 1, tek bir başarısız yoklamada "+
			"müdahale etmek demektir ve gözetmenin kendisi kesinti kaynağı olur",
			DefaultOptions.FailuresBeforeHeal)
	}
}

// TestFailureStreakResetsOnRecovery, ARADA bir başarılı ölçüm olduğunda
// sayacın sıfırlandığını doğrular.
//
// Sıfırlanmasaydı, saatler boyunca dağılmış üç bağımsız dalgalanma
// birikip sağlıklı bir uygulamayı yeniden başlatırdı.
func TestFailureStreakResetsOnRecovery(t *testing.T) {
	healer := &fakeHealer{healthy: false}
	h := newHarness(t, healer)

	h.cycles(DefaultOptions.FailuresBeforeHeal - 1) // eşiğin bir altı
	healer.healthy = true
	h.cycles(1) // araya giren BAŞARI
	healer.healthy = false
	h.cycles(DefaultOptions.FailuresBeforeHeal - 1) // yine eşiğin bir altı

	if healer.heals != 0 {
		t.Errorf("araya giren başarıya rağmen iyileştirildi (%d kez) — "+
			"sayaç sıfırlanmıyor", healer.heals)
	}
}

// TestUnhealthyAndHealedAreRecordedOnceEach, denetim zincirine yalnızca
// GEÇİŞLERİN yazıldığını doğrular.
//
// İki saniyede bir kayıt, zinciri gürültüyle doldurup `audit verify`
// çıktısını okunamaz hâle getirirdi.
func TestUnhealthyAndHealedAreRecordedOnceEach(t *testing.T) {
	healer := &fakeHealer{healthy: false, healFixes: true}
	h := newHarness(t, healer)

	h.cycles(DefaultOptions.FailuresBeforeHeal) // sağlıksız + iyileştir
	h.cycles(5)                                 // iyileşmiş, sağlıklı turlar

	got := h.audit.actions()
	want := []string{"app.unhealthy", "app.heal", "app.healed"}
	if len(got) != len(want) {
		t.Fatalf("denetim kayıtları %v, %v bekleniyordu", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("kayıt %d = %q, %q bekleniyordu", i, got[i], want[i])
		}
	}
}

// TestBackoffGrowsBetweenAttempts, sürekli çöken bir uygulamanın
// denemeler arası beklemesinin BÜYÜDÜĞÜNÜ doğrular.
//
// Sabit bir bekleme, bozuk imajlı bir uygulamayı sonsuza kadar saniyede
// bir yeniden başlatır ve günlükleri de CPU'yu da yer.
func TestBackoffGrowsBetweenAttempts(t *testing.T) {
	h := newHarness(t, &fakeHealer{healthy: false})

	var waits []time.Duration
	prev := 0
	for i := 0; i < 400 && len(waits) < 3; i++ {
		before := h.healer.heals
		h.sup.Cycle(context.Background())
		if h.healer.heals > before {
			waits = append(waits, time.Duration(i-prev)*DefaultOptions.Interval)
			prev = i
		}
		h.clock.now = h.clock.now.Add(DefaultOptions.Interval)
	}

	if len(waits) < 3 {
		t.Fatalf("üç deneme gözlenemedi: %v", waits)
	}
	if waits[1] >= waits[2] {
		t.Errorf("bekleme büyümüyor: %v — geri çekilme üstel olmalı", waits)
	}
}

// TestBackoffIsCapped, geri çekilmenin tavanı aşmadığını doğrular.
func TestBackoffIsCapped(t *testing.T) {
	h := newHarness(t, &fakeHealer{healthy: false})
	if got := h.sup.backoff(100); got != DefaultOptions.BackoffMax {
		t.Errorf("100. denemede bekleme %v, tavan %v olmalıydı — "+
			"tavansız kayma sonunda taşar", got, DefaultOptions.BackoffMax)
	}
}

// TestHealIsSkippedWhenActiveReleaseChanged, tur başında okunan sürüm
// bu arada değiştiyse iyileştirmenin YAPILMADIĞINI doğrular.
//
// Aynı anda bir dağıtım ya da geri alma trafiği taşımış olabilir. Kontrol
// olmasaydı, tam o sırada eskiyen bir sürümün konteynerlerini ayağa
// kaldırır ve boşaltmayla yarışırdık.
func TestHealIsSkippedWhenActiveReleaseChanged(t *testing.T) {
	h := newHarness(t, &fakeHealer{healthy: false})

	h.cycles(DefaultOptions.FailuresBeforeHeal - 1)
	// Araya bir dağıtım girdi: aktif sürüm artık başka.
	h.store.active = store.Deployment{AppID: testApp, ReleaseID: "r9"}
	h.cycles(1)

	if h.healer.heals != 0 {
		t.Error("aktif sürüm değişmişken iyileştirildi — " +
			"eskiyen sürümün konteynerleri boşaltmayla yarışırdı")
	}
}

// TestStateResetsWhenReleaseChanges, yeni bir sürümün önceki sürümün
// sayaçlarını MİRAS ALMADIĞINI doğrular.
func TestStateResetsWhenReleaseChanges(t *testing.T) {
	h := newHarness(t, &fakeHealer{healthy: false})

	h.cycles(DefaultOptions.FailuresBeforeHeal - 1)
	// Yeni sürüm canlıya alındı.
	h.store.deps[0].ReleaseID = "r4"
	h.cycles(1)

	if h.healer.heals != 0 {
		t.Error("yeni sürüm, eski sürümün başarısızlık sayacını miras aldı")
	}
}

// TestStateIsPrunedForRemovedApps, aktif olmayan uygulamaların
// durumunun bırakıldığını doğrular — aksi hâlde harita sınırsız büyür.
func TestStateIsPrunedForRemovedApps(t *testing.T) {
	h := newHarness(t, &fakeHealer{healthy: false})
	h.cycles(1)
	if len(h.sup.state) != 1 {
		t.Fatalf("durum girdisi %d, 1 bekleniyordu", len(h.sup.state))
	}

	h.store.deps = nil
	h.cycles(1)
	if len(h.sup.state) != 0 {
		t.Errorf("aktif olmayan uygulamanın durumu bırakılmadı: %d girdi", len(h.sup.state))
	}
}

// TestFailedHealIsRecordedAndBacksOff, iyileştirme hata verdiğinde
// denetime YAZILDIĞINI ve geri çekilmenin yine de ilerlediğini doğrular.
//
// İlerlemeseydi, her turda yeniden denenir ve bozuk bir uygulama
// gözetmeni sonsuz döngüye sokardı.
func TestFailedHealIsRecordedAndBacksOff(t *testing.T) {
	healer := &fakeHealer{healthy: false, healErr: errors.New("imaj yok")}
	h := newHarness(t, healer)

	h.cycles(DefaultOptions.FailuresBeforeHeal)
	if healer.heals != 1 {
		t.Fatalf("iyileştirme denemesi %d, 1 bekleniyordu", healer.heals)
	}
	h.cycles(DefaultOptions.FailuresBeforeHeal) // saat ilerlemedi → geri çekilme sürüyor
	if healer.heals != 1 {
		t.Errorf("geri çekilme sırasında yeniden denendi (%d) — sonsuz döngü riski",
			healer.heals)
	}

	var failed bool
	for _, r := range h.audit.records {
		if r.Action == "app.heal" && r.Outcome == audit.OutcomeFailure {
			failed = true
		}
	}
	if !failed {
		t.Errorf("başarısız iyileştirme denetime yazılmadı: %v", h.audit.actions())
	}
}

// TestConstructorRejectsIncompleteWiring, eksik bağımlılıkla gözetmen
// kurulamayacağını doğrular.
//
// Özellikle saat: nil bir saatle kurulabilseydi testler 30 saniyelik
// gerçek uykulara düşer ve tam da kararlılığın kritik olduğu yerde
// titrek olurdu.
func TestConstructorRejectsIncompleteWiring(t *testing.T) {
	ok := &fakeHealer{}
	st := &fakeStore{}
	au := &fakeAuditor{}
	al := &fakeAlarms{}
	cl := &fakeClock{}

	cases := map[string]func() (*Supervisor, error){
		"saat yok":         func() (*Supervisor, error) { return New(ok, st, au, al, nil, DefaultOptions) },
		"iyileştirici yok": func() (*Supervisor, error) { return New(nil, st, au, al, cl, DefaultOptions) },
		"depo yok":         func() (*Supervisor, error) { return New(ok, nil, au, al, cl, DefaultOptions) },
		"denetçi yok":      func() (*Supervisor, error) { return New(ok, st, nil, al, cl, DefaultOptions) },
		// Alarm hedefi de ZORUNLU: nil geçilebilseydi, sessiz arızaya
		// karşı yazılmış mekanizma sessizce kapatılabilirdi.
		"alarm yok": func() (*Supervisor, error) { return New(ok, st, au, nil, cl, DefaultOptions) },
		"aralık sıfır": func() (*Supervisor, error) {
			o := DefaultOptions
			o.Interval = 0
			return New(ok, st, au, al, cl, o)
		},
		"eşik sıfır": func() (*Supervisor, error) {
			o := DefaultOptions
			o.FailuresBeforeHeal = 0
			return New(ok, st, au, al, cl, o)
		},
		"alarm eşiği sıfır": func() (*Supervisor, error) {
			o := DefaultOptions
			o.HealsBeforeAlarm = 0
			return New(ok, st, au, al, cl, o)
		},
		"tavan tabandan küçük": func() (*Supervisor, error) {
			o := DefaultOptions
			o.BackoffMax = time.Second
			return New(ok, st, au, al, cl, o)
		},
	}
	for name, build := range cases {
		if _, err := build(); err == nil {
			t.Errorf("%s: kurulum kabul edildi, reddedilmeliydi", name)
		}
	}
}

// TestDefaultsFitTheThirtySecondCriterion, varsayılanların Faz 1 ölçütü
// #3'ün bütçesine sığdığını doğrular.
//
// ── Bu test neden var ───────────────────────────────────────────────
//
// Diğer bütün testler SAHTE saatle koşuyor, yani biri `Interval`'i 20
// saniyeye çıkarsa hepsi yeşil kalır ve ölçüt sessizce kaçırılır. Bu
// test, sayıların kendisine bakan tek yer.
func TestDefaultsFitTheThirtySecondCriterion(t *testing.T) {
	const criterion = 30 * time.Second

	detect := time.Duration(DefaultOptions.FailuresBeforeHeal) * DefaultOptions.Interval
	if detect > criterion/2 {
		t.Errorf("tespit %v sürüyor, ölçütün (%v) yarısından fazlası — "+
			"iyileştirmeye ve konteyner açılışına pay kalmıyor", detect, criterion)
	}
}

// healUntil, geri çekilmeyi aşarak n iyileştirme denemesi ürettirir.
//
// Saat elle ilerletiliyor: gözetmen bir sonraki denemeyi
// `nextHealAt`'e göre erteliyor ve gerçek saatle beklemek testi hem
// yavaş hem titrek yapardı.
func (h *harness) healUntil(n int) {
	for h.healer.heals < n {
		h.clock.now = h.clock.now.Add(DefaultOptions.BackoffMax)
		h.cycles(DefaultOptions.FailuresBeforeHeal)
	}
}

// TestAlarmOpensOnlyAfterThreshold, alarmın ERKEN çalmadığını doğrular.
//
// Tek bir başarısız iyileştirme, imaj çekilirken ya da host anlık yük
// altındayken de olur. Alarm hemen çalsaydı, kendiliğinden düzelen
// durumlar için gürültü üretir ve güvenilirliğini ilk günden yakardı.
func TestAlarmOpensOnlyAfterThreshold(t *testing.T) {
	healer := &fakeHealer{healthy: false, healErr: errors.New("imaj yok")}
	h := newHarness(t, healer)

	// Eşiğin BİR ALTINA kadar götür.
	h.healUntil(DefaultOptions.HealsBeforeAlarm - 1)
	if len(h.alarms.raised) != 0 {
		t.Fatalf("eşik aşılmadan alarm açıldı (%d iyileştirme): %d alarm",
			healer.heals, len(h.alarms.raised))
	}

	// Eşiği aş.
	h.healUntil(DefaultOptions.HealsBeforeAlarm)
	if len(h.alarms.raised) == 0 {
		t.Fatal("eşik aşıldı ama alarm AÇILMADI — gözetmen kurtaramıyor " +
			"ve kimsenin haberi yok")
	}

	got := h.alarms.raised[0]
	if got.Kind != "heal_exhausted" {
		t.Errorf("alarm türü %q", got.Kind)
	}
	if got.Target != testApp {
		t.Errorf("alarm hedefi %q, %q bekleniyordu", got.Target, testApp)
	}
	if got.Severity != store.SeverityCritical {
		t.Errorf("alarm ciddiyeti %q, kritik bekleniyordu", got.Severity)
	}
	// Hata metni SIZMAMALI: alarm ayrıntısı denetim zincirine de düşüyor
	// ve zincir ekle-sadece.
	if strings.Contains(got.Detail, "imaj yok") {
		t.Errorf("alarm ayrıntısı hata METNİNİ taşıyor: %q", got.Detail)
	}
}

// TestAlarmClearsWhenAppRecovers, düzelen uygulamanın alarmını
// kapattığını doğrular.
//
// Kapanmayan bir alarm, bakılmayan bir alarma dönüşür.
func TestAlarmClearsWhenAppRecovers(t *testing.T) {
	healer := &fakeHealer{healthy: false, healErr: errors.New("imaj yok")}
	h := newHarness(t, healer)

	h.healUntil(DefaultOptions.HealsBeforeAlarm)
	if len(h.alarms.raised) == 0 {
		t.Fatal("alarm açılmadı")
	}

	// Uygulama kendiliğinden düzeliyor.
	healer.healthy = true
	h.cycles(1)

	wantID := "heal_exhausted:" + testApp
	found := false
	for _, id := range h.alarms.cleared {
		if id == wantID {
			found = true
		}
	}
	if !found {
		t.Errorf("uygulama düzeldi ama alarm kapatılmadı: %v",
			h.alarms.cleared)
	}
}

// TestHealthyAppClearsAlarmEvenWithoutPriorFailure, kapatmanın
// `st.unhealthy` kontrolüne BAĞLI OLMADIĞINI doğrular.
//
// ── Neden bu ayrım önemli ───────────────────────────────────────────
//
// panelyd çöküp kalktığında bellekteki gözetim durumu sıfırlanır ama
// alarm satırı DİSKTE durur. Kapatmayı `st.unhealthy`'ye bağlasaydık,
// yeniden başlatmadan sonra sağlıklı dönen bir uygulamanın alarmı
// sonsuza kadar açık kalırdı — ve tam da o alarm, operatörün "bozuk"
// sandığı şey olurdu.
func TestHealthyAppClearsAlarmEvenWithoutPriorFailure(t *testing.T) {
	healer := &fakeHealer{healthy: true}
	h := newHarness(t, healer)

	h.cycles(2)

	if len(h.alarms.cleared) == 0 {
		t.Error("sağlıklı uygulama için hiç kapatma çağrılmadı — yeniden " +
			"başlatmadan sonra kalan alarm asla kapanmazdı")
	}
	if len(h.alarms.raised) != 0 {
		t.Errorf("sağlıklı uygulama alarm açtı: %v", h.alarms.raised)
	}
}

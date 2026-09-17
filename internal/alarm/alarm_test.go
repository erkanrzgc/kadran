package alarm

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/store"
)

// recorder, bildirilen olayları toplar.
type recorder struct{ events []Event }

func (r *recorder) Notify(_ context.Context, ev Event) {
	r.events = append(r.events, ev)
}

func (r *recorder) states() []Transition {
	out := make([]Transition, 0, len(r.events))
	for _, e := range r.events {
		out = append(out, e.State)
	}
	return out
}

// newManager, GERÇEK depoyla bir yönetici kurar.
//
// ⚠ Sahte depo KULLANILMIYOR ve bu zorunlu: bu paketin bütün iddiası
// "bir kez bildir" ve o iddiayı zorlayan şey birincil anahtar
// çakışması, yani ŞEMA. Sahte bir depoyla geçen test, şema kuralını
// hiç sınamadan yeşil olurdu.
//
// Dosya tabanlı: kalıcılık iddiası da sınanıyor.
func newManager(t *testing.T) (*Manager, *recorder, *store.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "panely.db")
	s, err := store.Open(context.Background(), path)
	if err != nil {
		t.Fatalf("depo açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	rec := &recorder{}
	return New(s, rec), rec, s, path
}

func sampleAlarm() store.Alarm {
	return store.Alarm{
		ID:       KindHealExhausted + ":pfprobe",
		Kind:     KindHealExhausted,
		Target:   "pfprobe",
		Severity: store.SeverityCritical,
		Since:    time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC),
		Detail:   "6 iyileştirme denemesi başarısız",
	}
}

// TestRaiseNotifiesOnlyOnce, BU PAKETİN ASIL İDDİASINI sınar.
//
// Gözetmen her turda Raise çağırıyor. Her çağrının bildirim üretmesi,
// bu projenin zaten ölümcül saydığı yanlış-alarm döngüsü olurdu:
// "tekrarlayan sahte alarmların sonu, gerçek olanın da yok sayılmasıdır."
func TestRaiseNotifiesOnlyOnce(t *testing.T) {
	m, rec, _, _ := newManager(t)
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		m.Raise(ctx, sampleAlarm())
	}

	if len(rec.events) != 1 {
		t.Fatalf("10 turda %d bildirim üretildi, 1 bekleniyordu: %v — "+
			"her tur bildiren bir alarm kapatılmaya mahkûmdur",
			len(rec.events), rec.states())
	}
	if rec.events[0].State != Opened {
		t.Errorf("ilk bildirim %q, %q bekleniyordu", rec.events[0].State, Opened)
	}
}

// TestClearIsSilentWhenNothingActive, hiç bozulmamış bir şey için
// "düzeldi" denmediğini doğrular.
func TestClearIsSilentWhenNothingActive(t *testing.T) {
	m, rec, _, _ := newManager(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		m.Clear(ctx, KindHealExhausted+":pfprobe")
	}

	if len(rec.events) != 0 {
		t.Errorf("etkin alarm yokken %d bildirim üretildi: %v",
			len(rec.events), rec.states())
	}
}

// TestFullCycleNotifiesBothEdges, açılış ve kapanışın ikisinin de
// bildirildiğini doğrular.
//
// Kapanış bildirimi olmadan operatör, alarmın hâlâ etkin olduğunu
// sanmaya devam ederdi.
func TestFullCycleNotifiesBothEdges(t *testing.T) {
	m, rec, _, _ := newManager(t)
	ctx := context.Background()
	a := sampleAlarm()

	m.Raise(ctx, a)
	m.Raise(ctx, a)
	m.Clear(ctx, a.ID)
	m.Clear(ctx, a.ID)
	m.Raise(ctx, a) // yeniden bozuldu

	want := []Transition{Opened, Closed, Opened}
	got := rec.states()
	if len(got) != len(want) {
		t.Fatalf("bildirimler %v, %v bekleniyordu", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d. bildirim %q, %q bekleniyordu", i, got[i], want[i])
		}
	}
}

// TestEscalationIsNotified, KÖTÜLEŞMENİN sessiz kalmadığını doğrular.
//
// "Bir kez bildir" kuralının açtığı boşluk tam burası: %9 boş diskle
// uyarı açılır, disk %3'e inince ikinci yükseltme yutulur ve alarm
// "uyari" olarak kalırdı.
func TestEscalationIsNotified(t *testing.T) {
	m, rec, s, _ := newManager(t)
	ctx := context.Background()

	warn := sampleAlarm()
	warn.ID = KindDiskLow + ":host"
	warn.Kind = KindDiskLow
	warn.Severity = store.SeverityWarning
	m.Raise(ctx, warn)

	crit := warn
	crit.Severity = store.SeverityCritical
	m.Raise(ctx, crit)
	m.Raise(ctx, crit) // tırmanma da BİR KEZ bildirilmeli

	want := []Transition{Opened, Escalated}
	got := rec.states()
	if len(got) != len(want) {
		t.Fatalf("bildirimler %v, %v bekleniyordu", got, want)
	}

	// Depodaki satır da tırmanmış olmalı — yalnızca bildirim değil.
	stored, err := s.GetAlarm(ctx, warn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Severity != store.SeverityCritical {
		t.Errorf("depodaki ciddiyet %q, %q bekleniyordu",
			stored.Severity, store.SeverityCritical)
	}
}

// TestNoDeescalation, kritikten uyarıya DÜŞÜLMEDİĞİNİ doğrular.
//
// Eşiğin etrafında gidip gelen bir disk aksi hâlde her turda ciddiyet
// değiştirip tam da kaçınılmak istenen gürültüyü üretirdi.
func TestNoDeescalation(t *testing.T) {
	m, rec, s, _ := newManager(t)
	ctx := context.Background()

	crit := sampleAlarm()
	crit.Severity = store.SeverityCritical
	m.Raise(ctx, crit)

	warn := crit
	warn.Severity = store.SeverityWarning
	m.Raise(ctx, warn)

	if len(rec.events) != 1 {
		t.Errorf("ciddiyet düşünce bildirim üretildi: %v", rec.states())
	}
	stored, err := s.GetAlarm(ctx, crit.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Severity != store.SeverityCritical {
		t.Errorf("ciddiyet %q'ya düştü — bir kez kritik olan koşul, "+
			"tamamen düzelene kadar kritik kalmalı", stored.Severity)
	}
}

// TestSinceSurvivesRepeatedRaise, "ne zamandır bozuk" cevabının
// kaymadığını doğrular.
//
// Üzerine yazan bir uygulama, üç gündür bozuk olan bir şeyi her turda
// "az önce bozuldu" diye gösterirdi — yani alarmın en çok bakılan
// alanı yalan söylerdi.
func TestSinceSurvivesRepeatedRaise(t *testing.T) {
	m, _, s, _ := newManager(t)
	ctx := context.Background()

	first := sampleAlarm()
	m.Raise(ctx, first)

	later := first
	later.Since = first.Since.Add(72 * time.Hour)
	m.Raise(ctx, later)

	stored, err := s.GetAlarm(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Since.Equal(first.Since) {
		t.Errorf("since %v'ye kaydı, %v bekleniyordu — alarm her turda "+
			"'az önce bozuldu' derdi", stored.Since, first.Since)
	}
}

// TestStateSurvivesRestart, kenar tetiklemenin panelyd'nin ÖMRÜNDEN
// UZUN yaşadığını doğrular.
//
// ── Neden bu testin olmaması tehlikeli ──────────────────────────────
//
// Durum bellekte tutulsaydı bütün diğer testler yine yeşil geçerdi:
// hepsi tek bir süreç içinde koşuyor. Ama `Restart=on-failure` ile
// çöküp kalkan bir daemon her açılışta BÜTÜN alarmları yeniden
// ateşlerdi — yani en çok gürültüyü tam da en kötü durumda üretirdi.
//
// Test bu yüzden depoyu KAPATIP yeniden açıyor ve YENİ bir yönetici
// kuruyor: süreç yeniden başlamış gibi.
func TestStateSurvivesRestart(t *testing.T) {
	m, rec, s, path := newManager(t)
	ctx := context.Background()
	a := sampleAlarm()

	m.Raise(ctx, a)
	if len(rec.events) != 1 {
		t.Fatalf("ilk yükseltme bildirmedi: %v", rec.states())
	}

	// ── Süreç yeniden başlıyor ───────────────────────────────────────
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(ctx, path)
	if err != nil {
		t.Fatalf("depo yeniden açılamadı: %v", err)
	}
	defer func() { _ = reopened.Close() }()

	rec2 := &recorder{}
	m2 := New(reopened, rec2)

	// Koşul hâlâ bozuk; gözetmen yine Raise çağırıyor.
	m2.Raise(ctx, a)

	if len(rec2.events) != 0 {
		t.Errorf("yeniden başlatmadan sonra alarm TEKRAR bildirildi: %v — "+
			"çökme döngüsündeki bir daemon her açılışta bütün alarmları "+
			"yeniden ateşlerdi", rec2.states())
	}

	// Ve düzelme hâlâ bildirilebilmeli.
	m2.Clear(ctx, a.ID)
	if len(rec2.events) != 1 || rec2.events[0].State != Closed {
		t.Errorf("yeniden başlatmadan sonra kapanış bildirilmedi: %v",
			rec2.states())
	}
}

// TestRaiseRejectsUnknownSeverity, şemaya sokulmadan önce ciddiyetin
// doğrulandığını sınar.
func TestRaiseRejectsUnknownSeverity(t *testing.T) {
	_, _, s, _ := newManager(t)
	ctx := context.Background()

	a := sampleAlarm()
	a.Severity = "belki"
	if _, err := s.RaiseAlarm(ctx, a); err == nil {
		t.Error("bilinmeyen ciddiyet kabul edildi")
	}
}

// TestDistinctTargetsAreDistinctAlarms, iki uygulamanın birbirinin
// alarmını bastırmadığını doğrular.
//
// Kimlik yalnızca TÜRDEN oluşsaydı, ikinci uygulamanın arızası
// birincisinin alarmıyla çakışır ve sessizce yutulurdu.
func TestDistinctTargetsAreDistinctAlarms(t *testing.T) {
	m, rec, s, _ := newManager(t)
	ctx := context.Background()

	a := sampleAlarm()
	b := sampleAlarm()
	b.ID = KindHealExhausted + ":web"
	b.Target = "web"

	m.Raise(ctx, a)
	m.Raise(ctx, b)

	if len(rec.events) != 2 {
		t.Errorf("iki ayrı uygulama için %d bildirim: %v",
			len(rec.events), rec.states())
	}
	list, err := s.ListAlarms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Errorf("etkin alarm sayısı %d, 2 bekleniyordu", len(list))
	}
}

// TestListOrdersCriticalFirst, listeye bakan kişinin önce en kötüyü
// gördüğünü doğrular.
func TestListOrdersCriticalFirst(t *testing.T) {
	m, _, s, _ := newManager(t)
	ctx := context.Background()

	warn := sampleAlarm()
	warn.ID = KindDiskLow + ":host"
	warn.Kind = KindDiskLow
	warn.Severity = store.SeverityWarning
	// Uyarı DAHA ESKİ: sıralama yalnızca zamana bakıyorsa öne geçer.
	warn.Since = time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	m.Raise(ctx, warn)

	crit := sampleAlarm()
	crit.Since = time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	m.Raise(ctx, crit)

	list, err := s.ListAlarms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("alarm sayısı %d", len(list))
	}
	if list[0].Severity != store.SeverityCritical {
		t.Errorf("listenin başı %q — kritik önde olmalı", list[0].Severity)
	}
}

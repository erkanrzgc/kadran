// Package alarm, arıza koşullarını KENAR TETİKLEMELİ olarak bildirir.
//
// ── Bu paketin var olma sebebi ──────────────────────────────────────
//
// Bir uygulama çökerse gözetmen onu kurtarıyor. Kurtaramazsa hiçbir şey
// olmuyor: journal'a bir satır düşüyor ve kimse bakmıyor. Sessiz arıza
// en pahalı arıza, çünkü maliyeti arızanın kendisi değil FARK EDİLENE
// KADAR GEÇEN SÜRE.
//
// ── Neden "tespit" ile "teslimat" ayrı ──────────────────────────────
//
// Bu paket alarmın NE ZAMAN çalacağına karar veriyor; nereye
// gideceğine değil. Teslimat ayrı bir karar ve ayrı bir engeli var:
// panelyd'nin systemd birimi `IPAddressDeny=any` taşıyor ve dışarı
// çıkamıyor (ÖLÇÜLDÜ — kısıtlı ortamda curl DNS bile çözemedi,
// kontrol grubunda aynı istek 302 döndü). Telegram/webhook için ya o
// politika gevşetilmeli ya ayrı bir gönderici süreç gerekli.
//
// Tespit o kararı BEKLEMEK ZORUNDA DEĞİL: koşullar, eşikler ve kenar
// tetikleme mantığı ağdan bağımsız. Teslimatı önce yapmak, hangi
// alarmların doğru olduğunu bilmeden boru döşemek olurdu.
package alarm

import (
	"context"
	"log/slog"

	"github.com/erkanrzgc/panely/internal/store"
)

// Alarm türleri.
//
// Kimlik "tür:hedef" biçiminde kuruluyor (ör. `heal_exhausted:pfprobe`),
// böylece aynı türden iki uygulama ayrı ayrı alarm üretebiliyor ama aynı
// uygulama iki kez üretemiyor.
const (
	KindHealExhausted     = "heal_exhausted"
	KindBackupFailed      = "backup_failed"
	KindProxyUnreconciled = "proxy_unreconciled"
	KindDiskLow           = "disk_low"
)

// Store, yöneticinin ihtiyaç duyduğu depo davranışıdır.
//
// Arayüz BURADA tanımlı, store paketinde değil: tüketici tarafında
// tanımlanan dar bir arayüz, testte sahtelemeyi kolaylaştırıyor ve
// paketin gerçekten neye dokunduğunu tek bakışta gösteriyor.
type Store interface {
	RaiseAlarm(ctx context.Context, a store.Alarm) (bool, error)
	ClearAlarm(ctx context.Context, id string) (bool, error)
	EscalateAlarm(ctx context.Context, id string) (bool, error)
}

// Sink, bir alarm olayını dışarı bildirir.
//
// Şu anki tek uygulama journal'a yazıyor. Telegram/webhook eklendiğinde
// buraya ikinci bir uygulama gelecek; tespit tarafı değişmeyecek.
type Sink interface {
	Notify(ctx context.Context, ev Event)
}

// Event, bildirilecek durum değişimidir.
type Event struct {
	Alarm store.Alarm
	// State, geçişin yönünü söyler.
	State Transition
}

// Transition, alarmın hangi yönde değiştiğini bildirir.
type Transition string

const (
	// Opened — koşul bozuk olmayan durumdan bozuk duruma geçti.
	Opened Transition = "acildi"
	// Escalated — zaten bozuktu, DAHA KÖTÜ oldu.
	Escalated Transition = "tirmandi"
	// Closed — koşul düzeldi.
	Closed Transition = "kapandi"
)

// Manager, alarm durumunu yönetir ve YALNIZCA geçişlerde bildirir.
type Manager struct {
	store Store
	sink  Sink
}

// New, bir yönetici kurar.
func New(s Store, sink Sink) *Manager {
	return &Manager{store: s, sink: sink}
}

// Raise, koşulun bozuk olduğunu bildirir.
//
// Her gözetim turunda çağrılmak ÜZERE tasarlandı: ikinci ve sonraki
// çağrılar sessizdir. Çağıran tarafın "daha önce haber verdim mi?"
// diye durum tutması GEREKMİYOR — o soru burada, veritabanında
// cevaplanıyor ve cevabı panelyd'nin ömründen uzun yaşıyor.
func (m *Manager) Raise(ctx context.Context, a store.Alarm) {
	fresh, err := m.store.RaiseAlarm(ctx, a)
	if err != nil {
		// Alarm yazılamıyorsa bunu YUTMAK, sessiz arızaya karşı
		// yazılmış bir mekanizmanın kendisini sessizce arızalı
		// bırakmak olurdu.
		slog.Error("ALARM YAZILAMADI — arıza görünmez kalabilir",
			"alarm", a.ID, "hata", err)
		return
	}
	if fresh {
		m.sink.Notify(ctx, Event{Alarm: a, State: Opened})
		return
	}

	// Zaten etkin. Tek istisna: kötüleşme.
	if a.Severity != store.SeverityCritical {
		return
	}
	climbed, err := m.store.EscalateAlarm(ctx, a.ID)
	if err != nil {
		slog.Error("alarm tırmandırılamadı", "alarm", a.ID, "hata", err)
		return
	}
	if climbed {
		m.sink.Notify(ctx, Event{Alarm: a, State: Escalated})
	}
}

// Clear, koşulun düzeldiğini bildirir.
//
// Raise gibi, her turda çağrılmak üzere tasarlandı: etkin alarm yoksa
// sessizdir. Sağlıklı bir sistemde her tur "her şey yolunda" demek,
// tam olarak kaçınılmak istenen gürültüdür.
func (m *Manager) Clear(ctx context.Context, id string) {
	wasActive, err := m.store.ClearAlarm(ctx, id)
	if err != nil {
		slog.Error("alarm kapatılamadı", "alarm", id, "hata", err)
		return
	}
	if wasActive {
		m.sink.Notify(ctx, Event{
			Alarm: store.Alarm{ID: id},
			State: Closed,
		})
	}
}

// LogSink, alarmları journal'a yazar.
//
// ── Neden ilk uygulama bu ───────────────────────────────────────────
//
// Teslimat kararı verilene kadar alarmların kaybolmaması gerekiyor.
// journal zaten toplanıyor, döndürülüyor ve `journalctl -u panelyd`
// ile okunabiliyor — yani sıfır ek altyapıyla çalışan bir hedef.
//
// Seviye ciddiyete göre seçiliyor: `-p err` ile süzen bir operatör
// yalnızca kritikleri görmeli.
type LogSink struct{}

// Notify, olayı journal'a yazar.
func (LogSink) Notify(_ context.Context, ev Event) {
	args := []any{
		"alarm", ev.Alarm.ID,
		"durum", string(ev.State),
	}
	if ev.State != Closed {
		args = append(args,
			"ciddiyet", ev.Alarm.Severity,
			"hedef", ev.Alarm.Target,
			"ayrinti", ev.Alarm.Detail)
	}

	if ev.State != Closed && ev.Alarm.Severity == store.SeverityCritical {
		slog.Error("ALARM", args...)
		return
	}
	slog.Warn("ALARM", args...)
}

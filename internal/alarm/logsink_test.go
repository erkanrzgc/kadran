package alarm

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/internal/store"
)

// ── Sözleşme: LogSink'in satırı ↔ Telegram göndericisi ───────────────
//
// deploy/notify/kadran-notify.sh, kadrand'nin journal'ındaki `msg=ALARM`
// satırlarını AYRIŞTIRIYOR (K-108). Biçim iki tarafta yazılı:
//   - burada, LogSink'in ürettiği satır,
//   - scripts/check-notify-format.sh'ta, "canlıdan" kopyalanmış örnekler.
// Aralarında bağ yoktu: LogSink'in biçimi değişse betiğin testleri yine
// geçer, Telegram teslimatı SESSİZCE dururdu. Bu test ikisini bağlıyor:
// LogSink'in çıktısı, betik testindeki satırla BAYT BAYT aynı olmalı.

// sinkCiktisi, LogSink'in satırını kadrand'nin kurduğu işleyicinin
// aynısıyla üretir (cmd/kadrand/main.go: slog.NewTextHandler, Level Info).
func sinkCiktisi(t *testing.T, ev Event) string {
	t.Helper()
	var buf bytes.Buffer
	eski := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(eski) })

	LogSink{}.Notify(context.Background(), ev)

	satir := strings.TrimSuffix(buf.String(), "\n")
	if strings.Count(satir, "\n") != 0 {
		t.Fatalf("tek satır bekleniyordu: %q", satir)
	}
	return zamaniAt(satir)
}

// zamaniAt, `time=...` alanını atar: betik örneklerinde zaman ya gerçek
// ya da `x`.
func zamaniAt(satir string) string {
	if strings.HasPrefix(satir, "time=") {
		if i := strings.IndexByte(satir, ' '); i > 0 {
			return satir[i+1:]
		}
	}
	return satir
}

// betikOrnekleri, check-notify-format.sh'taki ALARM satırlarını (zamansız)
// döndürür.
func betikOrnekleri(t *testing.T) map[string]bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "check-notify-format.sh"))
	if err != nil {
		t.Fatalf("betik okunamadı: %v", err)
	}
	out := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimSpace(l)
		if !strings.HasPrefix(l, "'time=") || !strings.Contains(l, "msg=ALARM") {
			continue
		}
		l = strings.TrimSuffix(strings.TrimPrefix(l, "'"), "' \\")
		out[zamaniAt(l)] = true
	}
	if len(out) < 5 {
		t.Fatalf("betikte yalnızca %d ALARM örneği bulundu — ayrıştırma bozuk, ölçüm geçersiz", len(out))
	}
	return out
}

func TestLogSinkLinesMatchTheNotifyScriptFixtures(t *testing.T) {
	ornekler := betikOrnekleri(t)
	durumlar := []struct {
		ad string
		ev Event
	}{
		{"kritik açılış, tırnaklı ayrıntı", Event{State: Opened, Alarm: store.Alarm{
			ID: "backup_failed:kadran.db", Target: "kadran.db", Severity: store.SeverityCritical,
			Detail: "zamanlı yedek alınamıyor — geri dönüş yolu YOK"}}},
		{"uyarı açılış", Event{State: Opened, Alarm: store.Alarm{
			ID: "disk_low:host", Target: "/", Severity: store.SeverityWarning,
			Detail: "%9 boş, 3.4 GiB / 38 GiB"}}},
		{"kapanış", Event{State: Closed, Alarm: store.Alarm{ID: "disk_low:host"}}},
		{"tırmanma", Event{State: Escalated, Alarm: store.Alarm{
			ID: "disk_low:host", Target: "host", Severity: store.SeverityCritical, Detail: "%3 boş"}}},
		{"kaçışlı tırnak", Event{State: Opened, Alarm: store.Alarm{
			ID: "heal_exhausted:web", Target: "web", Severity: store.SeverityCritical,
			Detail: `"web" 3 kez kurtarılamadı`}}},
		{"tırnaksız tek kelime", Event{State: Opened, Alarm: store.Alarm{
			ID: "disk_low:host", Target: "host", Severity: store.SeverityWarning, Detail: "dolmak"}}},
	}
	for _, d := range durumlar {
		got := sinkCiktisi(t, d.ev)
		if !ornekler[got] {
			t.Errorf("%s: LogSink'in satırı betik testinde YOK — biçim ayrışmış:\n  üretilen: %s", d.ad, got)
		}
	}
}

// TestLogSinkLevelFollowsSeverity: `journalctl -p err` ile süzen bir
// operatör yalnızca kritikleri görmeli; kapanış hiçbir zaman ERROR değil.
func TestLogSinkLevelFollowsSeverity(t *testing.T) {
	kritik := store.Alarm{ID: "a", Target: "h", Severity: store.SeverityCritical}
	uyari := store.Alarm{ID: "a", Target: "h", Severity: store.SeverityWarning}
	for _, d := range []struct {
		ev     Event
		seviye string
	}{
		{Event{State: Opened, Alarm: kritik}, "level=ERROR"},
		{Event{State: Escalated, Alarm: kritik}, "level=ERROR"},
		{Event{State: Opened, Alarm: uyari}, "level=WARN"},
		{Event{State: Closed, Alarm: kritik}, "level=WARN"},
	} {
		if got := sinkCiktisi(t, d.ev); !strings.HasPrefix(got, d.seviye+" ") {
			t.Errorf("%s/%s: %q, beklenen %s", d.ev.State, d.ev.Alarm.Severity, got, d.seviye)
		}
	}
}

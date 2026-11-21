package console

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/cli"
)

// spoofLine, inceleme bulgusundaki satır: ilk -domain başta, son (kazanan)
// -domain dolgunun ardında. Dolgu -env değerleri arıtmada *** olur.
func spoofLine(padding int) string {
	var b strings.Builder
	b.WriteString("app update -domain good.example -env A=1")
	for i := range padding {
		fmt.Fprintf(&b, " -env X%d=1", i)
	}
	b.WriteString(" -domain evil.example -env Z=1 web")
	return b.String()
}

func utf16Units(s string) int { return len(utf16.Encode([]rune(s))) }

var cutMarker = regexp.MustCompile(`\x{2026}|and \d+ more`)

// R10: kutu satırı kesmez; çalışacak olan son -domain kutuda görünür.
func TestBoxShowsWholeLine(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	line := spoofLine(30)
	words, err := ParseLine(line)
	if err != nil {
		t.Fatal(err)
	}
	redactedLine := Redact(words)
	if n := utf16Units(redactedLine); n <= 200 || n >= confirm.MaxConsoleLineUnits {
		t.Fatalf("dolgu yanlış: arıtılmış satır %d birim", n)
	}
	if _, exit := h.exec("hetzner", line); exit != cli.ExitOK {
		t.Fatalf("exit %d", exit)
	}
	detail := h.lastBox().Detail
	if !strings.Contains(detail, "evil.example") || !strings.Contains(detail, "good.example") {
		t.Fatalf("kutuda iki -domain de görünmeli:\n%s", detail)
	}
	if cutMarker.MatchString(detail) {
		t.Fatalf("kutuda kesme işareti:\n%s", detail)
	}
	// Devam satırları dört boşlukla girintili; kırma ve girinti kalkınca satır bütün.
	if !strings.Contains(strings.ReplaceAll(strings.ReplaceAll(detail, "\n    ", ""), "\n", ""), "Command: "+redactedLine) {
		t.Fatalf("satır bütün değil:\n%s", detail)
	}
	for i, row := range strings.Split(detail, "\n") {
		if utf16Units(row) > 96 {
			t.Fatalf("satır %d: %d birim > 96: %q", i, utf16Units(row), row)
		}
	}
	if !slices.Contains(h.lastCall().args, "evil.example") {
		t.Fatalf("çalışan komut: %q", h.lastCall().args)
	}
}

// Sığmayan satır yazılı ad ve kutudan ÖNCE reddedilir: Confirm yok, Run yok,
// done exit 1. Değiştirme, tehlikeli ve root sınıfının üçü de.
func TestTooLongLineRefusedBeforeBox(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("b", 1100)
	nbsp := strings.Repeat(string(rune(0xa0)), 170) // ham 170, kaçışlı 1020 birim
	lines := []string{
		"deploy web -branch " + long,
		"deploy web -branch " + nbsp,
		"app delete " + long,
		"key add -deploy " + long + " k.pub",
		"deploy web -branch " + strings.Repeat("b", confirm.MaxConsoleLineUnits-18),
	}
	for _, line := range lines {
		id, exit := h.exec("hetzner", line)
		if exit != cli.ExitError {
			t.Errorf("%.30s…: exit %d, want %d", line, exit, cli.ExitError)
		}
		if got := h.output(id, StreamErr); !strings.Contains(got, "This line is too long to confirm here; run it in the CLI.") {
			t.Errorf("%.30s…: stderr %q", line, got)
		}
	}
	if h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatalf("uzun satır ilerledi: Run %d, Confirm %d", h.callCount(), h.boxCount())
	}
	for _, e := range h.snapshot() {
		if e.Name == EventInput {
			t.Fatal("uzun satır yazılı ad istedi")
		}
	}
	// Sınırda (tam MaxConsoleLineUnits) kutu gösterilir.
	edge := "deploy web -branch " + strings.Repeat("b", confirm.MaxConsoleLineUnits-19)
	if n := utf16Units(edge); n != confirm.MaxConsoleLineUnits {
		t.Fatalf("sınır satırı %d birim", n)
	}
	if _, exit := h.exec("hetzner", edge); exit != cli.ExitOK || h.boxCount() != 1 {
		t.Fatalf("sınırdaki satır: exit %d, kutu %d", exit, h.boxCount())
	}
}

func TestTooLongMessageFollowsLanguage(t *testing.T) {
	h := newHarness(t, func(d *RunnerDeps) {
		d.Language = func() confirm.Language { return confirm.Turkish }
	})
	h.setAnswer(true)
	id, exit := h.exec("hetzner", "deploy web -branch "+strings.Repeat("b", 1100))
	want := "Bu satır burada onaylanamayacak kadar uzun; CLI'da çalıştırın."
	if exit != cli.ExitError || !strings.Contains(h.output(id, StreamErr), want) {
		t.Fatalf("exit %d stderr %q", exit, h.output(id, StreamErr))
	}
	if h.callCount() != 0 || h.boxCount() != 0 {
		t.Fatal("uzun satır ilerledi")
	}
	// Okuma sınıfı kutuya gitmez: uzunluk sınırı ona uygulanmaz.
	if _, exit := h.exec("hetzner", "logs "+strings.Repeat("b", 1100)); exit != cli.ExitOK {
		t.Fatalf("okuma komutu reddedildi: exit %d", exit)
	}
}

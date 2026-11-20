package console

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/cli"
)

func TestReadRunsWithoutConfirm(t *testing.T) {
	h := newHarness(t)
	_, exit := h.exec("hetzner", "status")
	if exit != cli.ExitOK {
		t.Fatalf("exit %d", exit)
	}
	if got := h.lastCall().args; !slices.Equal(got, []string{"status", "-s", "hetzner"}) {
		t.Fatalf("args %q", got)
	}
	if h.boxCount() != 0 {
		t.Fatal("okuma komutu onay kutusu açtı")
	}
}

func TestRunIDIsHex16(t *testing.T) {
	h := newHarness(t)
	id, _ := h.exec("hetzner", "status")
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(id) {
		t.Fatalf("run id %q", id)
	}
	id2, _ := h.exec("hetzner", "status")
	if id == id2 {
		t.Fatal("aynı kimlik iki kez")
	}
}

func TestChangeNeedsConfirm(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(false)
	_, exit := h.exec("hetzner", "deploy web")
	if exit != cli.ExitError {
		t.Fatalf("exit %d, want %d", exit, cli.ExitError)
	}
	if h.callCount() != 0 {
		t.Fatal("onaylanmayan değiştirme komutu çalıştı")
	}
	text := boxText(h.lastBox())
	if !strings.Contains(text, "hetzner") || !strings.Contains(text, "deploy web") {
		t.Fatalf("kutu metni:\n%s", text)
	}
	if strings.Contains(text, "-s hetzner") {
		t.Fatalf("eklenen -s kutuda görünmemeli:\n%s", text)
	}
	if hist := h.history("hetzner"); len(hist) == 0 || hist[len(hist)-1] != "deploy web" {
		t.Fatalf("geçmiş %q", hist)
	}
	h.setAnswer(true)
	if _, exit := h.exec("hetzner", "deploy web"); exit != cli.ExitOK {
		t.Fatalf("exit %d", exit)
	}
	if got := h.lastCall().args; !slices.Equal(got, []string{"deploy", "web", "-s", "hetzner"}) {
		t.Fatalf("args %q", got)
	}
}

func TestConfirmErrorRefuses(t *testing.T) {
	h := newHarness(t)
	h.mu.Lock()
	h.answerFn = func(confirm.Box) (bool, error) { return true, errors.New("dialog failed") }
	h.mu.Unlock()
	id, exit := h.exec("hetzner", "deploy web")
	if exit == cli.ExitOK || h.callCount() != 0 {
		t.Fatalf("hata dönen onay çalıştırdı: exit %d", exit)
	}
	if !strings.Contains(h.output(id, StreamErr), "dialog failed") {
		t.Fatalf("stderr %q", h.output(id, StreamErr))
	}
}

func TestConfirmBoxHidesEnv(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	h.exec("hetzner", "app create -env K=sir web")
	text := boxText(h.lastBox())
	if strings.Contains(text, "sir") || !strings.Contains(text, "K=***") {
		t.Fatalf("kutu metni:\n%s", text)
	}
	if !slices.Contains(h.lastCall().args, "K=sir") {
		t.Fatalf("gerçek değer komuta gitmeli: %q", h.lastCall().args)
	}
}

// -build-arg değeri kutuya ve geçmiş dosyasına girmez (confirm paketinin
// sözü: derleme argümanı değerleri kutuya hiç girmez); komuta gerçek değer gider.
func TestBuildArgValueNeverInBoxOrHistory(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	for _, line := range []string{
		"app create -build-arg NPM_TOKEN=s3cr3t web",
		"app create -build-arg=NPM_TOKEN=s3cr3t web",
		"app create --build-arg NPM_TOKEN=s3cr3t web",
		"app create --build-arg=NPM_TOKEN=s3cr3t web",
	} {
		if _, exit := h.exec("hetzner", line); exit != cli.ExitOK {
			t.Fatalf("%s: exit %d", line, exit)
		}
		text := boxText(h.lastBox())
		if strings.Contains(text, "s3cr3t") || !strings.Contains(text, "NPM_TOKEN=***") {
			t.Fatalf("%s: kutu metni:\n%s", line, text)
		}
		if !slices.ContainsFunc(h.lastCall().args, func(a string) bool { return strings.HasSuffix(a, "NPM_TOKEN=s3cr3t") }) {
			t.Fatalf("%s: gerçek değer komuta gitmeli: %q", line, h.lastCall().args)
		}
	}
	raw, err := os.ReadFile(filepath.Join(h.dir, historyFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cr3t") || !strings.Contains(string(raw), "NPM_TOKEN=***") {
		t.Fatalf("geçmiş dosyası: %s", raw)
	}
}

func TestSensitiveGoesToSecretEvent(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	if err := (AdminSetting{Dir: h.dir}).Save(true); err != nil {
		t.Fatal(err)
	}
	h.setRun(func(_ context.Context, _ []string, env cli.Env) int {
		_, _ = env.Stdout.Write([]byte("before\n"))
		_, _ = env.Sensitive.Write([]byte("SECRET-JBSWY\n"))
		_, _ = env.Stdout.Write([]byte("after\n"))
		return cli.ExitOK
	})
	for _, line := range []string{"totp enable", "totp reset"} {
		id := h.start("hetzner", line)
		h.waitInput(id)
		if err := h.r.Input(id, "hetzner"); err != nil {
			t.Fatal(err)
		}
		if exit := h.done(id); exit != cli.ExitOK {
			t.Fatalf("%s: exit %d", line, exit)
		}
		if out := h.output(id, ""); strings.Contains(out, "SECRET") || !strings.Contains(out, "secret output shown in a separate card") {
			t.Fatalf("%s: çıktı %q", line, out)
		}
		secretAt, beforeAt := -1, -1
		for i, e := range h.snapshot() {
			if d, ok := e.Data.(SecretData); ok && d.Run == id && strings.Contains(d.Data, "SECRET-JBSWY") {
				secretAt = i
			}
			if d, ok := e.Data.(OutputData); ok && d.Run == id && strings.Contains(d.Data, "before") {
				beforeAt = i
			}
		}
		if secretAt < 0 || beforeAt < 0 || beforeAt > secretAt {
			t.Fatalf("%s: gizli olay %d, önceki çıktı %d", line, secretAt, beforeAt)
		}
	}
	for _, name := range []string{historyFile, adminLogFile} {
		raw, err := os.ReadFile(filepath.Join(h.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "SECRET") {
			t.Fatalf("%s gizli değeri içeriyor", name)
		}
	}
}

func TestUseRefusedWhileRunning(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	blockUntilCancelled(h, release)
	id := h.start("hetzner", "status")
	h.waitStarted()
	if _, err := h.r.Run("hetzner", "use gcp"); !errors.Is(err, ErrBusy) {
		t.Fatalf("çalışırken use: %v", err)
	}
	close(release)
	h.done(id)
	if _, exit := h.exec("hetzner", "use gcp"); exit != cli.ExitOK {
		t.Fatalf("use exit %d", exit)
	}
	h.waitEvent("use", func(e Event) bool {
		d, ok := e.Data.(UseData)
		return e.Name == EventUse && ok && d.Server == "gcp"
	})
	if h.callCount() != 1 {
		t.Fatal("use komutu Run'a gitti")
	}
	if _, exit := h.exec("hetzner", "use nowhere"); exit == cli.ExitOK {
		t.Fatal("kayıtsız sunucuya use")
	}
}

func TestOneRunAtATime(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	blockUntilCancelled(h, release)
	id := h.start("hetzner", "status")
	h.waitStarted()
	for _, line := range []string{"status", "clear", "help"} {
		if _, err := h.r.Run("hetzner", line); !errors.Is(err, ErrBusy) {
			t.Fatalf("%s: %v", line, err)
		}
	}
	close(release)
	h.done(id)
	if _, err := h.r.Run("hetzner", "status"); err != nil {
		t.Fatalf("bitince yeni çalışma: %v", err)
	}
}

func TestCancelCancelsContext(t *testing.T) {
	h := newHarness(t)
	blockUntilCancelled(h, make(chan struct{}))
	id := h.start("hetzner", "logs -f web")
	h.waitStarted()
	if err := h.r.Cancel("0123456789abcdef"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("yanlış kimlik: %v", err)
	}
	if err := h.r.Cancel(id); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit != cli.ExitError {
		t.Fatalf("exit %d", exit)
	}
	if h.lastCall().ctx.Err() == nil {
		t.Fatal("bağlam iptal edilmedi")
	}
}

func TestShutdownCancelsRun(t *testing.T) {
	conns, _, _ := testConns(t)
	if _, _, err := conns.Dial(context.Background(), testTarget); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(d *RunnerDeps) { d.Conns = conns })
	blockUntilCancelled(h, make(chan struct{}))
	id := h.start("hetzner", "logs -f web")
	h.waitStarted()
	h.r.Shutdown()
	h.done(id)
	if h.lastCall().ctx.Err() == nil {
		t.Fatal("bağlam iptal edilmedi")
	}
	conns.mu.Lock()
	open := len(conns.conns)
	conns.mu.Unlock()
	if open != 0 {
		t.Fatalf("Shutdown bağlantıları kapatmadı: %d açık", open)
	}
	if _, err := h.r.Run("hetzner", "status"); !errors.Is(err, ErrClosed) {
		t.Fatalf("kapandıktan sonra Run: %v", err)
	}
	h.r.Shutdown()
}

func TestEmptyServerList(t *testing.T) {
	h := newHarness(t)
	h.mu.Lock()
	h.list = nil
	h.mu.Unlock()
	id, exit := h.exec("", "status")
	if exit == cli.ExitOK || !strings.Contains(h.output(id, StreamErr), "select a server with use") {
		t.Fatalf("status: exit %d stderr %q", exit, h.output(id, StreamErr))
	}
	// Arayüzden gelen kayıtsız ad seçili sunucu sayılmaz.
	id, exit = h.exec("hetzner", "status")
	if exit == cli.ExitOK || !strings.Contains(h.output(id, StreamErr), "select a server with use") {
		t.Fatalf("kayıtsız sunucu: exit %d stderr %q", exit, h.output(id, StreamErr))
	}
	if h.callCount() != 0 {
		t.Fatal("sunucusuz status çalıştı")
	}
	if _, exit := h.exec("", "help"); exit != cli.ExitOK || !slices.Equal(h.lastCall().args, []string{"help"}) {
		t.Fatalf("help: exit %d", exit)
	}
	if _, exit := h.exec("", "server list"); exit != cli.ExitOK || !slices.Equal(h.lastCall().args, []string{"server", "list"}) {
		t.Fatalf("server list: exit %d args %q", exit, h.lastCall().args)
	}
	id, exit = h.exec("", "use gcp")
	if exit == cli.ExitOK || !strings.Contains(h.output(id, StreamErr), "no saved server") {
		t.Fatalf("use: exit %d stderr %q", exit, h.output(id, StreamErr))
	}
}

func TestHelpAndClear(t *testing.T) {
	h := newHarness(t)
	h.setRun(func(_ context.Context, _ []string, env cli.Env) int {
		_, _ = env.Stderr.Write([]byte("USAGE-TEXT\n"))
		return cli.ExitOK
	})
	for _, line := range []string{"help", "-h", "--help", "kadran help"} {
		id, exit := h.exec("hetzner", line)
		if exit != cli.ExitOK || !slices.Equal(h.lastCall().args, []string{"help"}) {
			t.Fatalf("%s: exit %d args %q", line, exit, h.lastCall().args)
		}
		out := h.output(id, "")
		usage := strings.Index(out, "USAGE-TEXT")
		for _, want := range []string{"Enter", "↑/↓", "Tab", "Ctrl+C", "Ctrl+L", "Ctrl+`", "use <server>"} {
			if at := strings.Index(out, want); at < usage || usage < 0 {
				t.Fatalf("%s: %q yok ya da CLI yardımından önce:\n%s", line, want, out)
			}
		}
	}
	calls := h.callCount()
	if _, exit := h.exec("hetzner", "clear"); exit != cli.ExitOK {
		t.Fatalf("clear exit %d", exit)
	}
	h.waitEvent("clear", func(e Event) bool { return e.Name == EventClear })
	if h.callCount() != calls {
		t.Fatal("clear Run'a gitti")
	}
}

func TestHelpFlagRunsAsRead(t *testing.T) {
	h := newHarness(t)
	if _, exit := h.exec("hetzner", "deploy -h"); exit != cli.ExitOK {
		t.Fatalf("exit %d", exit)
	}
	if got := h.lastCall().args; !slices.Equal(got, []string{"deploy", "-h", "-s", "hetzner"}) {
		t.Fatalf("args %q", got)
	}
	if h.boxCount() != 0 {
		t.Fatal("yardım bayrağı onay istedi")
	}
	if _, exit := h.exec("hetzner", "app delete --help"); exit != cli.ExitOK || h.boxCount() != 0 {
		t.Fatalf("app delete --help: exit %d", exit)
	}
}

func TestStdinPromptCarriesCurrentLine(t *testing.T) {
	h := newHarness(t)
	var got string
	h.setRun(func(_ context.Context, _ []string, env cli.Env) int {
		_, _ = env.Stdout.Write([]byte("Signing in.\n"))
		_, _ = env.Stderr.Write([]byte("6-digit code from the app: "))
		buf := make([]byte, 3)
		var b strings.Builder
		for !strings.HasSuffix(b.String(), "\n") {
			n, err := env.Stdin.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				return cli.ExitError
			}
		}
		got = b.String()
		return cli.ExitOK
	})
	id := h.start("hetzner", "login")
	if in := h.waitInput(id); in.Prompt != "6-digit code from the app: " {
		t.Fatalf("istem %q", in.Prompt)
	}
	if err := h.r.Input(id, "424242"); err != nil {
		t.Fatal(err)
	}
	if exit := h.done(id); exit != cli.ExitOK || got != "424242\n" {
		t.Fatalf("exit %d got %q", exit, got)
	}
	inputs := 0
	for _, e := range h.snapshot() {
		if e.Name == EventInput {
			inputs++
		}
	}
	if inputs != 1 {
		t.Fatalf("%d istem olayı (kısa okuma yeni istem açmamalı)", inputs)
	}
	if h.boxCount() != 0 {
		t.Fatal("oturum komutu onay istedi")
	}
}

func TestInputValidation(t *testing.T) {
	h := newHarness(t)
	release := make(chan struct{})
	blockUntilCancelled(h, release)
	id := h.start("hetzner", "status")
	h.waitStarted()
	if err := h.r.Input(id, "x"); !errors.Is(err, ErrNotWaiting) {
		t.Fatalf("istem yokken: %v", err)
	}
	if err := h.r.Input("ffffffffffffffff", "x"); !errors.Is(err, ErrNoRun) {
		t.Fatalf("yanlış kimlik: %v", err)
	}
	if err := h.r.Input(id, "a\nb"); !errors.Is(err, ErrInputLine) {
		t.Fatalf("çok satır: %v", err)
	}
	close(release)
	h.done(id)
}

func TestHistoryRedacted(t *testing.T) {
	h := newHarness(t)
	h.setAnswer(true)
	h.exec("hetzner", "app create -env K=sir web")
	hist := h.history("hetzner")
	if last := hist[len(hist)-1]; last != "app create -env K=*** web" {
		t.Fatalf("geçmiş %q", last)
	}

	h.setRun(func(_ context.Context, _ []string, env cli.Env) int {
		buf := make([]byte, 64)
		_, _ = env.Stdin.Read(buf)
		return cli.ExitOK
	})
	id := h.start("hetzner", "login")
	h.waitInput(id)
	if err := h.r.Input(id, "424242"); err != nil {
		t.Fatal(err)
	}
	h.done(id)
	h.setRun(nil)
	id = h.start("hetzner", "prune -all")
	h.waitInput(id)
	if err := h.r.Input(id, "hetzner"); err != nil {
		t.Fatal(err)
	}
	h.done(id)
	for _, line := range h.history("hetzner") {
		if strings.Contains(line, "sir") || strings.Contains(line, "424242") || strings.Contains(line, "hetzner") {
			t.Fatalf("geçmişe gizli değer ya da girdi yazıldı: %q", line)
		}
	}
	if got := h.history("hetzner"); !slices.Equal(got, []string{"app create -env K=*** web", "login", "prune -all"}) {
		t.Fatalf("geçmiş %q", got)
	}
}

func TestPanicInCommandEndsRun(t *testing.T) {
	h := newHarness(t)
	h.setRun(func(context.Context, []string, cli.Env) int { panic("boom") })
	id, exit := h.exec("hetzner", "status")
	if exit != cli.ExitError || !strings.Contains(h.output(id, StreamErr), "internal error: boom") {
		t.Fatalf("exit %d stderr %q", exit, h.output(id, StreamErr))
	}
	h.setRun(nil)
	if _, exit := h.exec("hetzner", "status"); exit != cli.ExitOK {
		t.Fatalf("panikten sonra yuva boşalmadı: exit %d", exit)
	}
}

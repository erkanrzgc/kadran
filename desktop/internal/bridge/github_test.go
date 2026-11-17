package bridge

import (
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBridgeGitHubDisconnectNeedsConfirmation(t *testing.T) {
	// Yanlış onay: kutu açılmadan, sunucuya gidilmeden hata.
	hs := newHarness(t, true)
	for _, bad := range []string{"", "Disconnect", "blog", "disconnect "} {
		if _, err := hs.b.GitHubDisconnect(target, bad); err == nil || !strings.Contains(err.Error(), "confirmation") {
			t.Errorf("onay %q kabul edildi: %v", bad, err)
		}
	}
	if hs.h.boxCount() != 0 || hs.d.count() != 0 {
		t.Fatalf("kutu %d, çağrı %d", hs.h.boxCount(), hs.d.count())
	}

	// Kutuda vazgeçildi: Cancelled, sunucuya gidilmedi.
	no := newHarness(t, false)
	got, err := no.b.GitHubDisconnect(target, "disconnect")
	if err != nil || !reflect.DeepEqual(got, Cancelled{Cancelled: true}) {
		t.Fatalf("%v %v", got, err)
	}
	if no.h.boxCount() != 1 || no.d.count() != 0 {
		t.Fatalf("kutu %d, çağrı %d", no.h.boxCount(), no.d.count())
	}

	// Onaylandı: github.disconnect, onay sözcüğüyle.
	yes := newHarness(t, true)
	if _, err := yes.b.GitHubDisconnect(target, "disconnect"); err != nil {
		t.Fatal(err)
	}
	box := yes.h.boxes[0]
	if box.Title != "Disconnect GitHub" || box.Action != "Disconnect" || !strings.Contains(box.Message, target) {
		t.Errorf("%+v", box)
	}
	want := map[string]any{"target": target, "confirmation": "disconnect"}
	if yes.d.count() != 1 || yes.d.calls[0].method != "github.disconnect" || !reflect.DeepEqual(yes.d.calls[0].params, want) {
		t.Errorf("%+v", yes.d.calls)
	}
}

func TestGitHubStatusAndConnectNeverAsk(t *testing.T) {
	hs := newHarness(t, false)
	if _, err := hs.b.GitHubStatus(target); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.b.GitHubConnect(target); err != nil {
		t.Fatal(err)
	}
	if _, err := hs.b.GitHubStatus("-oProxyCommand=x"); err == nil {
		t.Error("kötü hedef kabul edildi")
	}
	var methods []string
	for _, c := range hs.d.calls {
		methods = append(methods, c.method)
	}
	if hs.h.boxCount() != 0 || strings.Join(methods, ",") != "github.status,github.connect" {
		t.Fatalf("kutu %d, %v", hs.h.boxCount(), methods)
	}
}

func TestOpenGitHubOpensOnlyAllowlistedAddresses(t *testing.T) {
	hs := newHarness(t, true)
	const ok = "https://github.com/login/device"
	if err := hs.b.OpenGitHub(ok); err != nil || len(hs.h.opened) != 1 || hs.h.opened[0] != ok {
		t.Fatalf("%v %v", err, hs.h.opened)
	}
	for _, bad := range []string{"https://evil.example/login/device", "file:///c:/x", "https://github.com/login/device?x=1", ""} {
		if err := hs.b.OpenGitHub(bad); err == nil || !strings.Contains(err.Error(), "only GitHub app addresses can be opened") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	if len(hs.h.opened) != 1 {
		t.Fatalf("izin verilmeyen adres açıldı: %v", hs.h.opened)
	}
}

func TestGitHubConnectRunsOneAtATime(t *testing.T) {
	hs := newHarness(t, true)
	hs.d.blockMethod = "github.connect"
	hs.d.unblock = make(chan struct{})
	hs.d.started = make(chan struct{}, 1)

	var wg sync.WaitGroup
	wg.Add(1)
	var firstErr error
	go func() {
		defer wg.Done()
		_, firstErr = hs.b.GitHubConnect(target)
	}()
	<-hs.d.started

	// Birincisi surerken ikinci cagri sunucuya gitmeden reddedilir.
	if _, err := hs.b.GitHubConnect(target); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Errorf("ikinci cagri kabul edildi: %v", err)
	}
	if hs.d.count() != 1 {
		t.Errorf("sunucuya %d cagri gitti", hs.d.count())
	}
	// Durum sorgusu kapiya takilmaz.
	if _, err := hs.b.GitHubStatus(target); err != nil {
		t.Errorf("durum sorgusu kapiya takildi: %v", err)
	}

	close(hs.d.unblock)
	wg.Wait()
	if firstErr != nil {
		t.Fatal(firstErr)
	}
	// Kapi birakildi: yeni baglanma yine calisir.
	if _, err := hs.b.GitHubConnect(target); err != nil {
		t.Errorf("kapi birakilmadi: %v", err)
	}
}

func TestOpenGitHubDebouncesOneSecond(t *testing.T) {
	hs := newHarness(t, true)
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.UTC)
	hs.b.deps.Now = func() time.Time { return now }
	const dev = "https://github.com/login/device"
	const inst = "https://github.com/settings/installations"

	if err := hs.b.OpenGitHub(dev); err != nil {
		t.Fatal(err)
	}
	// Ayni adres 1 sn icinde: cift tiklama tek sekme.
	now = now.Add(999 * time.Millisecond)
	if err := hs.b.OpenGitHub(dev); err != nil || len(hs.h.opened) != 1 {
		t.Fatalf("999 ms sonra ayni adres yutulmadi: %v %v", err, hs.h.opened)
	}
	// Yutulan cagri sureyi uzatmaz: ilk acistan 1 sn sonra acilir.
	now = now.Add(time.Millisecond)
	if err := hs.b.OpenGitHub(dev); err != nil || len(hs.h.opened) != 2 {
		t.Fatalf("1 sn sonra ayni adres acilmadi: %v %v", err, hs.h.opened)
	}
	// Farkli adres 250 ms tabanin altinda yutulur (sekme yagmuru siniri)...
	now = now.Add(249 * time.Millisecond)
	if err := hs.b.OpenGitHub(inst); err != nil || len(hs.h.opened) != 2 {
		t.Fatalf("249 ms sonra farkli adres yutulmadi: %v %v", err, hs.h.opened)
	}
	// ...ama 1 sn beklemez: giris sayfasindan hemen sonra kurulum baglantisi acilir.
	now = now.Add(time.Millisecond)
	if err := hs.b.OpenGitHub(inst); err != nil || len(hs.h.opened) != 3 || hs.h.opened[2] != inst {
		t.Fatalf("250 ms sonra farkli adres acilmadi: %v %v", err, hs.h.opened)
	}
	// Gecersiz adres hata verir, debounce'a bakilmaz.
	now = now.Add(time.Millisecond)
	if err := hs.b.OpenGitHub("https://evil.example/"); err == nil {
		t.Error("kotu adres kabul edildi")
	}
}

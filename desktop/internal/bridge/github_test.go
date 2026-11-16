package bridge

import (
	"reflect"
	"strings"
	"testing"
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

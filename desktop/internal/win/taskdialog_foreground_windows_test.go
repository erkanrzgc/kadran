package win

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// pfCallback ve lpCallbackData, paketli amd64 yerleşimde cxWidth'ten (156)
// önceki iki işaretçi: 140 ve 148. 124 (hFooterIcon) ve 132 (pszFooter)
// kullanılmaz; cxWidth ve öteki alanlar yerinde kalır.
func TestCallbackFieldsInLayout(t *testing.T) {
	const owner = windows.HWND(0x1234)
	cfg := layout(owner, dialogTexts(t))[:taskDialogConfigSize]
	le := binary.LittleEndian
	if taskDialogCallbackOffset != 140 || taskDialogCallbackDataOffset != 148 ||
		taskDialogCallbackDataOffset+8 != taskDialogWidthOffset {
		t.Fatal("pfCallback 140, lpCallbackData 148 olmalı; cxWidth (156) hemen ardından gelir")
	}
	if got := le.Uint64(cfg[taskDialogCallbackOffset:]); got == 0 || got != uint64(dialogCallbackPtr) {
		t.Fatalf("pfCallback %#x, paket düzeyindeki geri çağırma %#x olmalı", got, dialogCallbackPtr)
	}
	if got := le.Uint64(cfg[taskDialogCallbackDataOffset:]); got != uint64(owner) {
		t.Fatalf("lpCallbackData %#x, sahip pencere olmalı", got)
	}
	if le.Uint32(cfg[taskDialogWidthOffset:]) != 320 || le.Uint32(cfg[72:]) != buttonCancel {
		t.Fatal("cxWidth ya da varsayılan düğme değişti")
	}
	for _, b := range cfg[76:140] {
		if b != 0 {
			t.Fatal("kullanılmayan alanlar sıfır kalmalı")
		}
	}
}

type fakeDialogAPI struct {
	foreground windows.HWND
	calls      []string
}

func (f *fakeDialogAPI) Foreground() windows.HWND { return f.foreground }
func (f *fakeDialogAPI) SetTopmost(h windows.HWND) {
	f.calls = append(f.calls, fmt.Sprintf("topmost:%d", h))
}
func (f *fakeDialogAPI) ClearTopmost(h windows.HWND) {
	f.calls = append(f.calls, fmt.Sprintf("notopmost:%d", h))
}
func (f *fakeDialogAPI) SetForeground(h windows.HWND) bool {
	f.calls = append(f.calls, fmt.Sprintf("set:%d", h))
	return true
}
func (f *fakeDialogAPI) Flash(h windows.HWND) { f.calls = append(f.calls, fmt.Sprintf("flash:%d", h)) }

func TestOnCreatedRaisesDialogOnce(t *testing.T) {
	const dialog, owner, other = windows.HWND(7), windows.HWND(5), windows.HWND(9)
	cases := []struct {
		name       string
		owner      windows.HWND
		foreground windows.HWND
		want       string
	}{
		{"dialog got focus", owner, dialog, "topmost:7,notopmost:7,set:7"},
		{"another app is in front", owner, other, "topmost:7,notopmost:7,set:7,flash:5"},
		{"no owner", 0, other, "topmost:7,notopmost:7,set:7,flash:7"},
	}
	for _, c := range cases {
		api := &fakeDialogAPI{foreground: c.foreground}
		onCreated(dialog, c.owner, api)
		if got := strings.Join(api.calls, ","); got != c.want {
			t.Fatalf("%s: calls %s, want %s", c.name, got, c.want)
		}
	}
}

// Yalnız TDN_CREATED çalışır; öteki bildirimler hiçbir pencere API'sini
// çağırmaz ve S_OK (0) döner: geri çağırma düğmeye basmaz, kutuyu kapatmaz.
func TestNotificationsOtherThanCreatedAreIgnored(t *testing.T) {
	const dialog, owner = windows.HWND(7), windows.HWND(5)
	if tdnCreated != 0 {
		t.Fatal("TDN_CREATED 0 olmalı")
	}
	for msg := uintptr(0); msg <= 12; msg++ {
		api := &fakeDialogAPI{foreground: 9}
		if got := handleNotification(api, dialog, msg, uintptr(owner)); got != 0 {
			t.Fatalf("bildirim %d: dönüş %d, 0 olmalı", msg, got)
		}
		if msg != tdnCreated && len(api.calls) != 0 {
			t.Fatalf("bildirim %d API çağırdı: %v", msg, api.calls)
		}
		if msg == tdnCreated && len(api.calls) == 0 {
			t.Fatal("TDN_CREATED hiçbir şey yapmadı")
		}
	}
}

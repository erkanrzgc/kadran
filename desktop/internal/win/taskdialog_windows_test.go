package win

import (
	"encoding/binary"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func dialogTexts(t *testing.T) []*uint16 {
	t.Helper()
	out := make([]*uint16, 0, 5)
	for _, s := range []string{"Başlık", "İleti", "Ayrıntı", "Vazgeç", "Yedek oluştur"} {
		p, err := windows.UTF16PtrFromString(s)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// Enter varsayılan düğmeye basar: o düğme Vazgeç olmalı, yoksa Enter
// sunucuda işlemi başlatır. Esc'nin kutuyu kapatması da bayrağa bağlı.
func TestDialogDefaultsToCancel(t *testing.T) {
	texts := dialogTexts(t)
	mem := layout(windows.HWND(0x1234), texts)
	le := binary.LittleEndian
	cfg, buttons := mem[:taskDialogConfigSize], mem[taskDialogConfigSize:]

	if got := le.Uint32(cfg[72:]); got != buttonCancel {
		t.Fatalf("varsayılan düğme %d, Vazgeç (%d) olmalı", got, buttonCancel)
	}
	if le.Uint32(cfg[20:])&tdfAllowDialogCancellation == 0 {
		t.Fatal("Esc kutuyu kapatmıyor")
	}
	if le.Uint32(cfg[0:]) != taskDialogConfigSize || le.Uint64(cfg[4:]) != 0x1234 || le.Uint32(cfg[60:]) != 2 {
		t.Fatal("yapının boyu, sahibi ya da düğme sayısı yanlış")
	}
	if le.Uint64(cfg[64:]) != uint64(uintptr(unsafe.Pointer(&buttons[0]))) {
		t.Fatal("düğme dizisi yapının ardında değil")
	}
	if le.Uint32(buttons[0:]) != buttonCancel || le.Uint64(buttons[4:]) != uint64(uintptr(unsafe.Pointer(texts[3]))) {
		t.Fatal("ilk düğme Vazgeç değil")
	}
	if le.Uint32(buttons[12:]) != buttonAction || le.Uint64(buttons[16:]) != uint64(uintptr(unsafe.Pointer(texts[4]))) {
		t.Fatal("ikinci düğme eylem değil")
	}
	for off, i := range map[int]int{28: 0, 44: 1, 52: 2} {
		if le.Uint64(cfg[off:]) != uint64(uintptr(unsafe.Pointer(texts[i]))) {
			t.Fatalf("%d. bayttaki metin yanlış", off)
		}
	}
}

// cxWidth, paketli x64 yerleşimde son alan: 156. Yapı boyuyla (160) uyumlu.
const cxWidthOffset = 156

// Genişlik 0 iken Windows kendi genişliğini seçer ve etki satırı dağılır.
func TestTaskDialogWidth(t *testing.T) {
	mem := layout(windows.HWND(0x1234), dialogTexts(t))
	cfg := mem[:taskDialogConfigSize]
	if cxWidthOffset+4 != taskDialogConfigSize {
		t.Fatal("cxWidth yapının son alanı olmalı")
	}
	if got := binary.LittleEndian.Uint32(cfg[cxWidthOffset:]); got != 320 {
		t.Fatalf("cxWidth %d, 320 olmalı", got)
	}
}

type fakeForeground struct {
	foreground windows.HWND
	iconic     bool
	setOK      bool
	calls      []string
}

func (f *fakeForeground) Foreground() windows.HWND   { return f.foreground }
func (f *fakeForeground) IsIconic(windows.HWND) bool { return f.iconic }
func (f *fakeForeground) Restore(windows.HWND)       { f.calls = append(f.calls, "restore") }
func (f *fakeForeground) SetForeground(windows.HWND) bool {
	f.calls = append(f.calls, "set")
	return f.setOK
}
func (f *fakeForeground) Flash(windows.HWND) { f.calls = append(f.calls, "flash") }

func TestBringOwnerForward(t *testing.T) {
	const owner = windows.HWND(0x1234)
	cases := []struct {
		name string
		api  fakeForeground
		want []string
	}{
		{"already in front", fakeForeground{foreground: owner, iconic: true}, nil},
		{"behind", fakeForeground{foreground: 0x99, setOK: true}, []string{"set"}},
		{"minimized", fakeForeground{foreground: 0x99, iconic: true, setOK: true}, []string{"restore", "set"}},
		{"foreground lock", fakeForeground{foreground: 0x99, iconic: true}, []string{"restore", "set", "flash"}},
	}
	for _, c := range cases {
		api := c.api
		bringForward(owner, &api)
		if strings.Join(api.calls, ",") != strings.Join(c.want, ",") {
			t.Fatalf("%s: calls %v, want %v", c.name, api.calls, c.want)
		}
	}
}

// Kapalı başarısızlık: yalnız eylem düğmesi onaydır.
func TestOnlyTheActionButtonConfirms(t *testing.T) {
	const idCancel = 2 // Esc ve kapatma düğmesi
	for _, pressed := range []int32{0, idCancel, buttonCancel} {
		if accepted(pressed) {
			t.Fatalf("%d onay sayıldı", pressed)
		}
	}
	if !accepted(buttonAction) {
		t.Fatal("eylem düğmesi onay sayılmadı")
	}
}

// FLASHWINFO amd64'te 32 bayttır; yanlış boyut FlashWindowEx'i sessizce bozar.
func TestFlashInfoSize(t *testing.T) {
	if got := unsafe.Sizeof(flashInfo{}); got != 32 {
		t.Fatalf("flashInfo boyutu %d, 32 olmalı", got)
	}
}

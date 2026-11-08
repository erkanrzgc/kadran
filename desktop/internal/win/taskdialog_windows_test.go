package win

import (
	"encoding/binary"
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

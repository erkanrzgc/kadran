package win

import (
	"testing"
	"unsafe"
)

func TestPolicyOnAndOff(t *testing.T) {
	on := Policy(true, 0xa0f7f4f4)
	if on != (AccentPolicy{State: 4, Flags: 2, Color: 0xa0f7f4f4}) {
		t.Fatalf("%+v", on)
	}
	if off := Policy(false, 0xa0f7f4f4); off.State != 0 {
		t.Fatalf("%+v", off)
	}
	// Windows'un ACCENT_POLICY yapısı dört 32 bitlik alan.
	if unsafe.Sizeof(on) != 16 {
		t.Fatalf("boyut %d", unsafe.Sizeof(on))
	}
}

func TestBuildIsReadable(t *testing.T) {
	if Build() < 10240 {
		t.Fatalf("Windows yapısı okunamadı: %d", Build())
	}
}

func TestConfirmLayoutConstants(t *testing.T) {
	// TASKDIALOGCONFIG pshpack1 ile 160, TASKDIALOG_BUTTON 12 bayt (amd64).
	if taskDialogConfigSize != 160 || taskDialogButtonSize != 12 {
		t.Fatal("görev kutusu yerleşimi değişti")
	}
}

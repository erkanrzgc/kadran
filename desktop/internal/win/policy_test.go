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

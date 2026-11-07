// Package win, masaüstünün Windows'a özgü çağrıları: pencerenin camı,
// yerel onay kutusu ve sistem sürümü. Wails'i içe aktarmaz; pencere
// kimliğiyle (HWND) çalışır.
package win

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows camı: pencere odakta olmasa da masaüstü arkada bulanık kalır.
// Sistemin arka plan malzemesi (DWMWA_SYSTEMBACKDROP_TYPE) odak gidince
// griye düşer ve bunu değiştirmenin yolu yok; SetWindowCompositionAttribute
// düşmez (K-161). Çağrı belgelenmemiş: bir Windows güncellemesi kaldırırsa
// Backdrop'a geçilir.
const (
	wcaAccentPolicy               = 19
	accentDisabled                = 0
	accentEnableAcrylicBlurBehind = 4
	accentFlagGradientColor       = 2
	dwmwaWindowCornerPreference   = 33
	dwmwcpRound                   = 2
	dwmwaSystemBackdropType       = 38
	dwmsbtTransientWindow         = 3 // Acrylic
)

var (
	user32          = windows.NewLazySystemDLL("user32.dll")
	dwmapi          = windows.NewLazySystemDLL("dwmapi.dll")
	setComposition  = user32.NewProc("SetWindowCompositionAttribute")
	enumWindows     = user32.NewProc("EnumWindows")
	isWindowVisible = user32.NewProc("IsWindowVisible")
	setDwmAttr      = dwmapi.NewProc("DwmSetWindowAttribute")
)

// AccentPolicy, SetWindowCompositionAttribute'un yükü.
type AccentPolicy struct {
	State       int32
	Flags       int32
	Color       uint32
	AnimationID int32
}

type compositionData struct {
	Attribute int32
	Data      *AccentPolicy
	Size      uintptr
}

// Policy, cam açıkken Akrilik, ton temaya göre (AABBGGRR); kapalıyken efekt yok.
func Policy(on bool, tint uint32) AccentPolicy {
	state := int32(accentDisabled)
	if on {
		state = accentEnableAcrylicBlurBehind
	}
	return AccentPolicy{State: state, Flags: accentFlagGradientColor, Color: tint}
}

// AccentAvailable, SetWindowCompositionAttribute bu sistemde var mı.
func AccentAvailable() bool { return setComposition.Find() == nil }

// OwnWindow, bu sürecin görünür üst düzey penceresi. Wails pencere kimliğini
// vermiyor; pencere görünmeden önce çağrılırsa bulunamaz (6 Kasım denemesi).
func OwnWindow() (windows.HWND, error) {
	pid := uint32(os.Getpid())
	var found windows.HWND
	cb := syscall.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		var owner uint32
		if _, err := windows.GetWindowThreadProcessId(h, &owner); err != nil || owner != pid {
			return 1
		}
		if visible, _, _ := isWindowVisible.Call(uintptr(h)); visible == 0 {
			return 1
		}
		found = h
		return 0
	})
	_, _, _ = enumWindows.Call(cb, 0)
	if found == 0 {
		return 0, errors.New("the window is not visible yet")
	}
	return found, nil
}

func setDwm(hwnd windows.HWND, attr uint32, value int32) error {
	r, _, _ := setDwmAttr.Call(uintptr(hwnd), uintptr(attr), uintptr(unsafe.Pointer(&value)), 4)
	if r != 0 {
		return fmt.Errorf("DwmSetWindowAttribute(%d) failed: 0x%x", attr, r)
	}
	return nil
}

// ApplyAccent, camı tonuyla açar. Köşe isteği başarısız olsa da cam çalışır.
func ApplyAccent(hwnd windows.HWND, tint uint32) error {
	_ = setDwm(hwnd, dwmwaWindowCornerPreference, dwmwcpRound)
	policy := Policy(true, tint)
	data := compositionData{Attribute: wcaAccentPolicy, Data: &policy, Size: unsafe.Sizeof(policy)}
	r, _, callErr := setComposition.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&data)))
	if r == 0 {
		return fmt.Errorf("SetWindowCompositionAttribute refused: %w", callErr)
	}
	return nil
}

// ApplyBackdrop, sistemin Akrilik malzemesi (yedek; odak gidince griye düşer).
func ApplyBackdrop(hwnd windows.HWND) error {
	_ = setDwm(hwnd, dwmwaWindowCornerPreference, dwmwcpRound)
	return setDwm(hwnd, dwmwaSystemBackdropType, dwmsbtTransientWindow)
}

// Build, Windows yapı numarası (Windows 11 22H2 = 22621). Uyumluluk
// katmanından etkilenmez (RtlGetVersion).
func Build() int {
	return int(windows.RtlGetVersion().BuildNumber)
}

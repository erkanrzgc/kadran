package win

import (
	"encoding/binary"
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Yerel onay kutusu: TaskDialogIndirect.
//
// Wails'in MessageDialog'u Windows'ta düz MessageBox: düğmeler Evet/Hayır,
// ad verilemiyor ve varsayılan düğme seçilemiyor. Electron'daki kutu ise
// "Vazgeç" / "Kalıcı olarak sil" gibi adlı düğmeler gösteriyor ve Vazgeç
// varsayılan. Bu yüzden görev kutusu doğrudan çağrılıyor (K-165). Ortak
// denetimlerin 6. sürümünü ister: ikilideki manifest (rsrc_windows_amd64.syso).

var (
	comctl32           = windows.NewLazySystemDLL("comctl32.dll")
	taskDialogIndirect = comctl32.NewProc("TaskDialogIndirect")
)

const (
	tdfAllowDialogCancellation  = 0x0008
	tdfPositionRelativeToWindow = 0x1000
	tdWarningIcon               = 0xFFFF // MAKEINTRESOURCEW(-1)
	taskDialogConfigSize        = 160    // pshpack1: alanlar arasında dolgu yok
	taskDialogButtonSize        = 12
	buttonCancel, buttonAction  = 1001, 1002
)

// Box, kutunun metinleri.
type Box struct {
	Title, Message, Detail, Cancel, Action string
}

// utf16, metnin sıfırla biten UTF-16 kopyası (yığında: adresi değişmez).
func utf16(s string) (*uint16, error) {
	return windows.UTF16PtrFromString(s)
}

// Confirm, kutuyu owner penceresine bağlı gösterir; true yalnız eylem
// düğmesine basılınca. Esc, kapatma ya da hata: false (kapalı başarısızlık).
func Confirm(owner windows.HWND, box Box) (bool, error) {
	if err := taskDialogIndirect.Find(); err != nil {
		return false, fmt.Errorf("the confirmation dialog is not available: %w", err)
	}
	texts := make([]*uint16, 0, 5)
	for _, s := range []string{box.Title, box.Message, box.Detail, box.Cancel, box.Action} {
		p, err := utf16(s)
		if err != nil {
			return false, fmt.Errorf("the confirmation text is invalid: %w", err)
		}
		texts = append(texts, p)
	}
	mem := layout(owner, texts)

	// Kutu kendi ileti döngüsünü bu iş parçacığında döndürür.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var pressed int32
	hr, _, _ := taskDialogIndirect.Call(uintptr(unsafe.Pointer(&mem[0])), uintptr(unsafe.Pointer(&pressed)), 0, 0)
	runtime.KeepAlive(texts)
	runtime.KeepAlive(mem)
	if int32(hr) < 0 {
		return false, fmt.Errorf("the confirmation dialog failed: 0x%x", uint32(hr))
	}
	return accepted(pressed), nil
}

// accepted: onay yalnız eylem düğmesi. Esc ve kapatma IDCANCEL (2) döner.
func accepted(pressed int32) bool { return pressed == buttonAction }

// layout, TASKDIALOGCONFIG ve iki düğme; texts sırası başlık, ileti,
// ayrıntı, vazgeç, eylem. Yapı ve düğmeler TEK blokta: blok çağrıya uintptr
// olarak gider ve LazyProc.Call (go:uintptrescapes) yüzünden yığında değil
// heap'te durur; düğmelerin adresi çağrı boyunca değişmez.
func layout(owner windows.HWND, texts []*uint16) []byte {
	title, message, detail, cancel, action := texts[0], texts[1], texts[2], texts[3], texts[4]
	mem := make([]byte, taskDialogConfigSize+2*taskDialogButtonSize)
	cfg, buttons := mem[:taskDialogConfigSize], mem[taskDialogConfigSize:]
	le := binary.LittleEndian

	// TASKDIALOG_BUTTON (paketli): int nButtonID, PCWSTR pszButtonText.
	le.PutUint32(buttons[0:], buttonCancel)
	le.PutUint64(buttons[4:], uint64(uintptr(unsafe.Pointer(cancel))))
	le.PutUint32(buttons[12:], buttonAction)
	le.PutUint64(buttons[16:], uint64(uintptr(unsafe.Pointer(action))))

	// TASKDIALOGCONFIG (paketli, amd64 yerleşimi).
	le.PutUint32(cfg[0:], taskDialogConfigSize)
	le.PutUint64(cfg[4:], uint64(owner))
	le.PutUint32(cfg[20:], tdfAllowDialogCancellation|tdfPositionRelativeToWindow)
	le.PutUint64(cfg[28:], uint64(uintptr(unsafe.Pointer(title))))
	le.PutUint64(cfg[36:], tdWarningIcon)
	le.PutUint64(cfg[44:], uint64(uintptr(unsafe.Pointer(message))))
	le.PutUint64(cfg[52:], uint64(uintptr(unsafe.Pointer(detail))))
	le.PutUint32(cfg[60:], 2)
	le.PutUint64(cfg[64:], uint64(uintptr(unsafe.Pointer(&buttons[0]))))
	le.PutUint32(cfg[72:], buttonCancel) // varsayılan düğme: Vazgeç
	return mem
}

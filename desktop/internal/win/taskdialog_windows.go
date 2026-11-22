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
	// cxWidth, paketli yapının son alanı (156..160); diyalog birimi. 0 iken
	// Windows kendi genişliğini seçip etki satırını yeniden sarıyordu.
	taskDialogWidthOffset = 156
	taskDialogWidth       = 320
	// pfCallback ve lpCallbackData, cxWidth'ten (156) önceki iki işaretçi;
	// pshpack1 amd64: ... pszFooter 132..140, pfCallback 140..148,
	// lpCallbackData 148..156.
	taskDialogCallbackOffset     = 140
	taskDialogCallbackDataOffset = 148
	tdnCreated                   = 0 // TDN_CREATED: kutunun penceresi oluştu
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
	bringForward(owner, realForeground{})

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
	le.PutUint64(cfg[taskDialogCallbackOffset:], uint64(dialogCallbackPtr))
	le.PutUint64(cfg[taskDialogCallbackDataOffset:], uint64(owner))
	le.PutUint32(cfg[taskDialogWidthOffset:], taskDialogWidth)
	return mem
}

// foregroundAPI, kutu açılmadan önce sahip pencereyi öne getiren küçük yüzey;
// mantık gerçek pencere olmadan sınanabilsin diye arayüz.
type foregroundAPI interface {
	Foreground() windows.HWND
	IsIconic(windows.HWND) bool
	Restore(windows.HWND)
	SetForeground(windows.HWND) bool
	Flash(windows.HWND)
}

// bringForward: kutu görünmeden komut "çalışıyor"da beklemesin. Kutuyu atlamaz
// ve onaylamaz; yalnız sahibi öne getirir, Windows ön plan kilidi izin
// vermezse görev çubuğunda yanıp söndürür.
func bringForward(owner windows.HWND, api foregroundAPI) {
	if owner == 0 || api.Foreground() == owner {
		return
	}
	if api.IsIconic(owner) {
		api.Restore(owner)
	}
	if !api.SetForeground(owner) {
		api.Flash(owner)
	}
}

// dialogAPI, kutunun kendi penceresini öne getiren yüzey (TDN_CREATED).
type dialogAPI interface {
	Foreground() windows.HWND
	SetTopmost(windows.HWND)
	ClearTopmost(windows.HWND)
	SetForeground(windows.HWND) bool
	Flash(windows.HWND)
}

// dialogCallbackPtr, TASKDIALOGCONFIG.pfCallback: paket düzeyinde BİR kez
// kurulur (NewCallback sınırlıdır; her Confirm'de yenisi kurulmaz).
var dialogCallbackPtr = windows.NewCallback(dialogCallback)

// dialogCallback: refData, layout'un lpCallbackData'ya koyduğu sahip pencere.
func dialogCallback(hwnd, msg, _, _, refData uintptr) uintptr {
	return handleNotification(realForeground{}, windows.HWND(hwnd), msg, refData)
}

// handleNotification yalnız TDN_CREATED'e tepki verir ve her zaman S_OK (0)
// döner: düğmeye basmaz, kutuyu kapatmaz, varsayılan düğmeye dokunmaz.
func handleNotification(api dialogAPI, dialog windows.HWND, msg, refData uintptr) uintptr {
	if msg == tdnCreated {
		onCreated(dialog, windows.HWND(refData), api)
	}
	return 0
}

// onCreated: kutu başka bir uygulamanın arkasında doğmasın. Z-sırasının
// en üstüne BİR kez çıkarır (kalıcı sabitlemez), odak ister; ön plan kilidi
// izin vermezse sahibi (yoksa kutuyu) görev çubuğunda yanıp söndürür.
func onCreated(dialog, owner windows.HWND, api dialogAPI) {
	api.SetTopmost(dialog)
	api.ClearTopmost(dialog)
	api.SetForeground(dialog)
	if api.Foreground() == dialog {
		return
	}
	if owner == 0 {
		owner = dialog
	}
	api.Flash(owner)
}

var (
	setWindowPosProc        = user32.NewProc("SetWindowPos")
	getForegroundWindowProc = user32.NewProc("GetForegroundWindow")
	isIconicProc            = user32.NewProc("IsIconic")
	showWindowProc          = user32.NewProc("ShowWindow")
	setForegroundWindowProc = user32.NewProc("SetForegroundWindow")
	flashWindowExProc       = user32.NewProc("FlashWindowEx")
)

const (
	swpNoSize       = 0x1
	swpNoMove       = 0x2
	swpNoActivate   = 0x10
	swRestore       = 9
	flashwAll       = 0x3
	flashwTimerNoFG = 0xC
)

// flashInfo, FLASHWINFO (amd64: cbSize 4, dolgu 4, hwnd 8, flags 4, count 4, timeout 4, dolgu 4).
type flashInfo struct {
	size    uint32
	hwnd    windows.HWND
	flags   uint32
	count   uint32
	timeout uint32
}

type realForeground struct{}

func (realForeground) Foreground() windows.HWND {
	h, _, _ := getForegroundWindowProc.Call()
	return windows.HWND(h)
}

func (realForeground) IsIconic(h windows.HWND) bool {
	r, _, _ := isIconicProc.Call(uintptr(h))
	return r != 0
}

func (realForeground) Restore(h windows.HWND) { _, _, _ = showWindowProc.Call(uintptr(h), swRestore) }

func (realForeground) SetForeground(h windows.HWND) bool {
	r, _, _ := setForegroundWindowProc.Call(uintptr(h))
	return r != 0
}

func (realForeground) Flash(h windows.HWND) {
	fi := flashInfo{hwnd: h, flags: flashwAll | flashwTimerNoFG}
	fi.size = uint32(unsafe.Sizeof(fi))
	_, _, _ = flashWindowExProc.Call(uintptr(unsafe.Pointer(&fi)))
}

// HWND_TOPMOST (-1) ve HWND_NOTOPMOST (-2).
func (realForeground) SetTopmost(h windows.HWND)   { setZ(h, ^uintptr(0)) }
func (realForeground) ClearTopmost(h windows.HWND) { setZ(h, ^uintptr(1)) }

func setZ(h windows.HWND, after uintptr) {
	_, _, _ = setWindowPosProc.Call(uintptr(h), after, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

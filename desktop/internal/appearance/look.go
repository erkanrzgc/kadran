package appearance

// Material, pencerenin yerel malzemesi; arkada masaüstü bulanık görünür.
//
//   - accent: Windows camı (SetWindowCompositionAttribute), odak gidince de kalır.
//   - acrylic: sistemin arka plan malzemesi; accent uygulanamazsa. Odak
//     gidince griye düşer.
//   - none: malzeme yok, çerçeve opak.
//
// Arayüz bu değere göre çerçeveyi saydam ya da opak çizer.
type Material string

const (
	Accent  Material = "accent"
	Acrylic Material = "acrylic"
	None    Material = "none"
)

// win11Build, sistem arka plan malzemesinin geldiği Windows 11 22H2 yapısı.
const win11Build = 22621

// MaterialFor, bu sistemde denenecek malzeme. Windows 11'de önce camın
// kendisi (accent) denenir; uygulanamazsa acrylic'e, o da olmazsa none'a
// düşülür (main). Başka sistemlerde opak.
func MaterialFor(goos string, windowsBuild int) Material {
	if goos == "windows" && windowsBuild >= win11Build {
		return Accent
	}
	return None
}

// solidFrame, malzeme yokken çerçevenin opak rengi; tokens.css'teki
// --frame-solid ile aynı.
var solidFrame = map[string]RGBA{
	"dark":  {R: 0x0a, G: 0x0a, B: 0x0a, A: 0xff},
	"light": {R: 0xed, G: 0xed, B: 0xf0, A: 0xff},
}

// RGBA, pencerenin zemin rengi.
type RGBA struct{ R, G, B, A uint8 }

// WindowLook, pencerenin yerel tarafı; arayüz renkleri styles/tokens.css'te.
type WindowLook struct {
	Material   Material
	Background RGBA
	Scheme     string
}

// Look, tema ve malzemeden pencerenin görünümü. Cam her temada çerçevede:
// malzemesi olmayan sistemde pencere çerçevenin opak rengiyle açılır.
func Look(theme string, material Material) WindowLook {
	if theme != "dark" {
		theme = "light"
	}
	if material == None {
		return WindowLook{Material: None, Background: solidFrame[theme], Scheme: theme}
	}
	return WindowLook{Material: material, Background: RGBA{}, Scheme: theme}
}

// Change, tema ya da ayar kaydı değişince pencereye yeniden verilecekler.
// Yazı tipi ya da dil kaydı da buradan geçer; aynısını yeniden vermek
// titretir, bu yüzden değişmeyen alan false döner.
type Change struct {
	Background bool
	Accent     bool
}

// Diff, iki görünüm arasındaki değişiklik.
func Diff(prev, next WindowLook) Change {
	return Change{
		Background: prev.Background != next.Background,
		Accent:     next.Material == Accent && next.Scheme != prev.Scheme,
	}
}

// GlassTint, tema başına camın tonu (AABBGGRR). Kullanıcı Electron deneme
// penceresinde seçti (6 Kasım, K-163): açıkta buzlu beyaz, koyuda hafif
// koyu. Saydamlığı sıfır olamaz: pencere simsiyah çizilir.
func GlassTint(scheme string) uint32 {
	if scheme == "dark" {
		return 0x30181816
	}
	return 0xa0f7f4f4
}

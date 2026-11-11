package main

import (
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
)

// Wails'in çerçeve süsü her odaklanmada DwmExtendFrameIntoClientArea
// çağırıyor; Akrilik düz tona dönüyor (8 Kasım, açık temada opak çerçeve).
func TestGlassWindowHasNoFrameDecorations(t *testing.T) {
	for _, material := range []appearance.Material{appearance.Accent, appearance.Acrylic} {
		for _, theme := range []string{"light", "dark"} {
			o := windowOptions(appearance.Look(theme, material), `C:\data`)
			if !o.DisableFramelessWindowDecorations {
				t.Errorf("%s/%s: çerçeve süsü camı bozar", material, theme)
			}
			if !o.WebviewIsTransparent {
				t.Errorf("%s/%s: WebView saydam değilse cam görünmez", material, theme)
			}
		}
	}
}

func TestSolidWindowKeepsFrameDecorations(t *testing.T) {
	o := windowOptions(appearance.Look("light", appearance.None), `C:\data`)
	if o.DisableFramelessWindowDecorations || o.WebviewIsTransparent {
		t.Fatalf("malzemesiz pencere opak ve süslü kalmalı: %+v", o)
	}
}

func TestWindowOptionsFollowTheTheme(t *testing.T) {
	cases := map[string]windows.Theme{"dark": windows.Dark, "light": windows.Light, "glass": windows.Light}
	for theme, want := range cases {
		if got := windowOptions(appearance.Look(theme, appearance.Accent), `C:\data`).Theme; got != want {
			t.Errorf("%s: tema %v, beklenen %v", theme, got, want)
		}
	}
	o := windowOptions(appearance.Look("light", appearance.Accent), `C:\data`)
	if o.WebviewUserDataPath != filepath.Join(`C:\data`, "WebView2") || o.WindowIsTranslucent || o.BackdropType != windows.None {
		t.Fatalf("pencere ayarı değişti: %+v", o)
	}
}

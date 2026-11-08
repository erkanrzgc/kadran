// Kadran masaüstü: Wails v2 kabuğu (K-165).
//
// Tek ikili: arayüz (React, Vite ile derlenmiş) gömülü, sunucuyla konuşan
// dağıtıcı (internal/sidecar) süreç içinde. Arayüze yalnız bridge.Bridge'in
// yöntemleri açılır.
//
// Derleme (desktop/ içinde): önce `npm run build`, sonra
//
//	go build -tags desktop,production -trimpath -ldflags "-H windowsgui" -o Kadran.exe .
package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
	"github.com/erkanrzgc/kadran/desktop/internal/bridge"
	"github.com/erkanrzgc/kadran/desktop/internal/headers"
	"github.com/erkanrzgc/kadran/desktop/internal/redact"
	"github.com/erkanrzgc/kadran/desktop/internal/win"
)

//go:embed all:out/renderer
var assets embed.FS

// userDataName, ayarların dizini: Electron'un kullandığıyla aynı (K-165).
const userDataName = "kadran-desktop"

// singleInstanceID, ikinci kopyanın tanıdığı ad: aynı listeye iki pencere
// birden yazmasın.
const singleInstanceID = "kadran-desktop-6a1f0c3e"

func main() {
	if err := run(); err != nil {
		log.Printf("kadran: %v", err)
		os.Exit(1)
	}
}

// warn, günlüğe satır yazar. İleti sunucudan gelen metin taşıyabilir:
// denetim karakterleri ve sırlar arıtılır (satır enjeksiyonu yok).
func warn(message string) {
	log.Printf("kadran: %s", redact.Text(message, "")) //nolint:gosec // redact.Text denetim karakterlerini atıyor
}

func run() error {
	config, err := os.UserConfigDir()
	if err != nil {
		return fmt.Errorf("could not find the user config directory: %w", err)
	}
	userData := filepath.Join(config, userDataName)
	instanceID := singleInstanceID
	if dir := smokeUserData(); dir != "" {
		// Duman testi kullanıcının açık penceresine bağlanmasın.
		userData, instanceID = dir, singleInstanceID+"-smoke"
	}
	front, err := fs.Sub(assets, "out/renderer")
	if err != nil {
		return err
	}

	material := appearance.MaterialFor(goruntime.GOOS, win.Build())
	if material == appearance.Accent && !win.AccentAvailable() {
		material = appearance.Acrylic
	}
	if smokeBuild {
		// Duman testi yerel malzeme istemez: görüntü ve CI aynı kalsın.
		material = appearance.None
	}
	settings := appearance.Store{Dir: userData}
	look := appearance.Look(settings.Load(warn).Theme, material)

	dispatcher := newDispatcher(warn)
	defer dispatcher.Close()
	h := &host{warn: warn, material: material, look: look}
	b := bridge.New(bridge.Deps{
		Dispatcher: dispatcher,
		Host:       h,
		Settings:   settings,
		Material:   func() appearance.Material { return material },
		Fixture:    smokeBuild,
		Warn:       warn,
	})

	bg := look.Background
	return wails.Run(&options.App{
		Title:            "KΛDRΛN",
		Width:            1280,
		Height:           820,
		MinWidth:         760,
		MinHeight:        520,
		Frameless:        true,
		BackgroundColour: &options.RGBA{R: bg.R, G: bg.G, B: bg.B, A: bg.A},
		AssetServer:      &assetserver.Options{Assets: front, Middleware: func(next http.Handler) http.Handler { return headers.Middleware(smokeMiddleware(next)) }},
		OnStartup:        h.startup,
		OnDomReady: func(ctx context.Context) {
			h.domReady(ctx)
			startSmoke(ctx, h)
		},
		Bind: []any{b},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId:               instanceID,
			OnSecondInstanceLaunch: func(options.SecondInstanceData) { h.focus() },
		},
		// Pencereye bırakılan dosya WebView'i o dosyaya götürürdü.
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
		Windows:     windowOptions(look, userData),
	})
}

// windowOptions, pencerenin Windows'a özgü ayarları.
//
// Cam yalnız SetWindowCompositionAttribute'tan (Electron'daki gibi); Wails'in
// yarı saydam penceresi kapalı (6 Kasım denemesi). Camlı pencerede Wails'in
// çerçeve süsü de kapalı: süs için her odaklanmada
// DwmExtendFrameIntoClientArea çağırıyor ve bu Akriliği düz tona çeviriyor
// (K-163'te Electron'da ölçüldü; 8 Kasım'da gerçek pencerede açık temada
// çerçeve opak kaldı, süs kapalıyken iki temada da cam geldi).
func windowOptions(look appearance.WindowLook, userData string) *windows.Options {
	glass := look.Material != appearance.None
	theme := windows.Light
	if look.Scheme == "dark" {
		theme = windows.Dark
	}
	return &windows.Options{
		WebviewIsTransparent:              glass,
		DisableFramelessWindowDecorations: glass,
		WindowIsTranslucent:               false,
		BackdropType:                      windows.None,
		Theme:                             theme,
		WebviewUserDataPath:               filepath.Join(userData, "WebView2"),
	}
}

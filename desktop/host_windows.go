package main

import (
	"context"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/desktop/internal/win"
)

// host, köprünün pencere ve işletim sistemi tarafı (bridge.Host).
type host struct {
	warn func(string)

	mu       sync.Mutex
	ctx      context.Context
	hwnd     windows.HWND
	material appearance.Material
	look     appearance.WindowLook
	// glassOff: cam uygulanamadı, yedek malzemeye düşüldü; ton değişimi yok.
	glassOff bool
}

func (h *host) startup(ctx context.Context) {
	h.mu.Lock()
	h.ctx = ctx
	h.mu.Unlock()
}

func (h *host) context() context.Context {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ctx
}

// window, pencerenin kimliği; görünür olunca bir kez bulunur.
func (h *host) window() windows.HWND {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hwnd == 0 {
		if hwnd, err := win.OwnWindow(); err == nil {
			h.hwnd = hwnd
		}
	}
	return h.hwnd
}

// domReady, camı pencere görünür olduktan sonra uygular: daha önce
// pencere yok (6 Kasım denemesinde ilk deneme boşa düştü).
func (h *host) domReady(context.Context) {
	if h.material == appearance.None {
		return
	}
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if hwnd := h.window(); hwnd != 0 {
				h.applyGlass(hwnd)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		h.warn("the window did not become visible; the glass is not applied")
	}()
}

// applyGlass: önce Windows camı (odak gidince de kalır); Windows reddederse
// sistemin Akriliği, cam tamamen gitmesin.
func (h *host) applyGlass(hwnd windows.HWND) {
	h.mu.Lock()
	scheme := h.look.Scheme
	h.mu.Unlock()
	if h.material == appearance.Accent {
		err := win.ApplyAccent(hwnd, appearance.GlassTint(scheme))
		if err == nil {
			return
		}
		h.warn(err.Error())
	}
	h.mu.Lock()
	h.glassOff = true
	h.mu.Unlock()
	if err := win.ApplyBackdrop(hwnd); err != nil {
		h.warn(err.Error())
	}
}

func (h *host) Minimise()       { runtime.WindowMinimise(h.context()) }
func (h *host) ToggleMaximise() { runtime.WindowToggleMaximise(h.context()) }
func (h *host) Close()          { runtime.Quit(h.context()) }

// SetLook: tema değişince malzeme yeniden kurulmaz; yalnız opak zeminin
// rengi ve Windows camının tonu değişir. Aynısını yeniden vermek titretir.
func (h *host) SetLook(next appearance.WindowLook) {
	h.mu.Lock()
	change := appearance.Diff(h.look, next)
	h.look = next
	glassOff := h.glassOff
	h.mu.Unlock()
	ctx := h.context()
	if next.Scheme == "dark" {
		runtime.WindowSetDarkTheme(ctx)
	} else {
		runtime.WindowSetLightTheme(ctx)
	}
	if change.Background {
		bg := next.Background
		runtime.WindowSetBackgroundColour(ctx, bg.R, bg.G, bg.B, bg.A)
	}
	if change.Accent && !glassOff {
		if hwnd := h.window(); hwnd != 0 {
			if err := win.ApplyAccent(hwnd, appearance.GlassTint(next.Scheme)); err != nil {
				h.warn(err.Error())
			}
		}
	}
}

func (h *host) SetClipboard(text string) error {
	return runtime.ClipboardSetText(h.context(), text)
}

// Confirm, yerel görev kutusu; pencereye bağlı ve kipli.
func (h *host) Confirm(box confirm.Box) (bool, error) {
	return win.Confirm(h.window(), win.Box{Title: box.Title, Message: box.Message, Detail: box.Detail, Cancel: box.Cancel, Action: box.Action})
}

// focus, ikinci kopya açılmak istenince var olan pencereyi öne getirir.
func (h *host) focus() {
	ctx := h.context()
	if ctx == nil {
		return
	}
	runtime.WindowUnminimise(ctx)
	runtime.WindowShow(ctx)
}

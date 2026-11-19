package main

import (
	"context"
	"fmt"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/erkanrzgc/kadran/desktop/internal/appearance"
	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/desktop/internal/console"
	"github.com/erkanrzgc/kadran/internal/cli"
	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
)

// newConsoleRunner, konsolun çalıştırıcısını kurar (Kadran Konsolu).
//
//   - Onay: yalnız h.Confirm (varsayılan düğme İptal). Duman bayrağı atlatmaz.
//   - Sunucu listesi: CLI'ın sıfır servers.Store'uyla aynı servers.json; yoksa
//     -s <seçili> çözülmez ya da kutu ile çalışan kayıt ayrışır.
//   - Dizin: dir, masaüstünün kendi ayar (görünüm) dosyasının dizini; geçmiş,
//     yönetici ayarı ve yönetici kaydı onun yanında, ayrı dosyalarda durur.
//   - Bağlantı: masaüstünün dağıtıcısıyla aynı DialWith, konsol penceresi
//     açtırmadan.
func newConsoleRunner(dir string, settings appearance.Store, h *host) *console.Runner {
	conns := console.NewConns(func(t client.Target) (*client.Client, error) {
		return client.DialWith(t, client.DialOptions{HideConsole: true})
	})
	list := servers.Store{}.Load
	return console.NewRunner(console.RunnerDeps{
		Run:     cli.Run,
		Confirm: h.Confirm,
		Emit:    h.emit,
		Conns:   conns,
		History: console.History{Dir: dir},
		Admin:   console.AdminSetting{Dir: dir},
		Log:     console.AdminLog{Dir: dir},
		Servers: list,
		Language: func() confirm.Language {
			return confirm.Language(settings.Load(warn).Language)
		},
		Apps: func(ctx context.Context, server string) ([]string, error) {
			return listApps(ctx, conns, list, server)
		},
		Now: time.Now,
	})
}

// listApps, kayıtlı sunucunun uygulama adları; konsolun kendi bağlantısı
// üzerinden (Tab tamamlama).
func listApps(ctx context.Context, conns *console.Conns, list func() ([]servers.Server, error), name string) ([]string, error) {
	all, err := list()
	if err != nil {
		return nil, err
	}
	saved, ok := servers.Find(all, name)
	if !ok {
		return nil, fmt.Errorf("no saved server %q", name)
	}
	conn, _, err := conns.Dial(ctx, saved.Target)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	resp, err := conn.RPC().ListApps(ctx, &kadranv1.ListAppsRequest{})
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.GetApps()))
	for _, app := range resp.GetApps() {
		names = append(names, app.GetSpec().GetAppId())
	}
	return names, nil
}

// emit, konsol olayını arayüze yollar. Runner bunu çıktı kilidini TUTARKEN
// çağırır: engellenmez ve Runner'a geri çağrı yapmaz. Pencere henüz yoksa
// (OnStartup'tan önce) olay düşer.
func (h *host) emit(e console.Event) {
	if ctx := h.context(); ctx != nil {
		runtime.EventsEmit(ctx, e.Name, e.Data)
	}
}

//go:build windows && !smoke

package main

import (
	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/internal/client"
)

// Dağıtılan ikilide konsol gerçek bağlantıyı ve gerçek onay kutusunu kullanır;
// duman düzeneği console_smoke.go'da, yalnız `-tags smoke` ile derlenir.

// consoleDial, masaüstünün dağıtıcısıyla aynı DialWith; konsol penceresi
// açtırmaz.
func consoleDial() func(client.Target) (*client.Client, error) {
	return func(t client.Target) (*client.Client, error) {
		return client.DialWith(t, client.DialOptions{HideConsole: true})
	}
}

// consoleConfirm, konsolun tek onay yolu: yerel görev kutusu.
func consoleConfirm(h *host) func(confirm.Box) (bool, error) { return h.Confirm }

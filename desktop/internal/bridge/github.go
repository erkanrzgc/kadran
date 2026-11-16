package bridge

import (
	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/desktop/internal/guard"
)

// GitHub bağlantısı (K-167). Durum ve bağlanma yöntemleri onay istemez:
// bağlanma yalnız bir akış başlatır, kullanıcı github.com'da onaylayana
// kadar hiçbir şey değişmez.

// GitHubStatus, sunucunun GitHub bağlantı durumu.
func (b *Bridge) GitHubStatus(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("github.status", map[string]any{"target": t})
}

// GitHubConnect, cihaz akışını başlatır ve kullanıcıya gösterilecek kodu döner.
func (b *Bridge) GitHubConnect(target string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	return b.dispatch("github.connect", map[string]any{"target": t})
}

// GitHubDisconnect, bağlantıyı keser; onay sözcüğü TAM "disconnect" olmalı
// ve yerel onay kutusundan geçer. Kapı sunucu başına tek: aynı anda iki kez
// kesilemez.
func (b *Bridge) GitHubDisconnect(target, confirmation string) (any, error) {
	t, err := guard.Target(target)
	if err != nil {
		return nil, err
	}
	if err := guard.GitHubDisconnectConfirmation(confirmation); err != nil {
		return nil, err
	}
	return b.mutate("$github", confirm.Request{Kind: confirm.KindGitHubDisconnect, Target: t}, "github.disconnect",
		map[string]any{"target": t, "confirmation": confirmation})
}

// OpenGitHub, yalnız GitHub bağlantı akışının dört adres biçimini tarayıcıda
// açar (guard.GitHubURL); başka adres hata.
func (b *Bridge) OpenGitHub(url string) error {
	u, err := guard.GitHubURL(url)
	if err != nil {
		return err
	}
	return b.deps.Host.OpenURL(u)
}

package bridge

import (
	"time"

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
	// Aynı anda tek bağlanma: tekrarlanan çağrı sunucuda her seferinde yeni
	// cihaz kodu üretip GitHub hız sınırını yerdi.
	release, err := b.enter(t, githubConnectGate)
	if err != nil {
		return nil, err
	}
	defer release()
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
	if !b.openAllowed(u) {
		return nil
	}
	return b.deps.Host.OpenURL(u)
}

const (
	// githubConnectGate, GitHubConnect'in işlem kapısı adı ("$" ile başlayan
	// adlar gerçek uygulama kimliği olamaz).
	githubConnectGate = "$github-connect"
	// openGitHubDebounce, AYNI adresin iki açışı arasındaki en kısa süre
	// (çift tıklama tek sekme açsın).
	openGitHubDebounce = time.Second
	// openGitHubMinGap, FARKLI adresler arasındaki en kısa süre: akışta kullanıcı
	// giriş sayfasını açıp hemen kurulum bağlantısına basabilir; bu taban yalnız
	// ele geçmiş bir arayüzün sekme yağdırmasını sınırlar.
	openGitHubMinGap = 250 * time.Millisecond
)

// openAllowed, açışa izin varsa true döner ve onu kaydeder; yoksa çağrı
// sessizce yutulur. Aynı adres 1 sn, farklı adres 250 ms bekler.
func (b *Bridge) openAllowed(u string) bool {
	now := time.Now
	if b.deps.Now != nil {
		now = b.deps.Now
	}
	b.gateMu.Lock()
	defer b.gateMu.Unlock()
	t := now()
	if !b.lastOpen.IsZero() {
		gap := openGitHubMinGap
		if u == b.lastOpenURL {
			gap = openGitHubDebounce
		}
		if t.Sub(b.lastOpen) < gap {
			return false
		}
	}
	b.lastOpen, b.lastOpenURL = t, u
	return true
}

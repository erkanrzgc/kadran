package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"sync"
	"time"
)

const (
	refreshPath = "/login/oauth/access_token"
	// refreshMargin: erişim jetonunun bitimine bundan az kaldıysa önce yenilenir.
	// Sınırda (tam 5 dakika kaldığında) yenilenmez: koşul `<`, `<=` değil.
	refreshMargin = 5 * time.Minute
	// maxExpiry, GitHub yanıtındaki süreler için üst sınırdır (saçma değerlere karşı).
	maxExpiry = 10 * 365 * 24 * time.Hour
)

var (
	ErrNotConnected = errors.New("github: not connected")
	ErrReconnect    = errors.New("github: connection lost: reconnect GitHub")
)

// saveStateFn testlerin kaydetme hatasını zorlayabilmesi için bir kancadır.
var saveStateFn = saveState

// tokens bellekteki bağlantı durumunu ve yenilemeyi tek uçuşta tutar: mu
// ağ çağrısı boyunca tutulur, bekleyenler taze jetonu görür.
type tokens struct {
	mu       sync.Mutex
	path     string
	clientID string
	c        *client
	now      func() time.Time
	st       state
	ok       bool
	// reconnect: ErrReconnect yüzünden bağlantı düştü; yeni bağlantı kaydedilene kadar true.
	reconnect bool
}

type refreshResponse struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int64  `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int64  `json:"refresh_token_expires_in"`
	Error            string `json:"error"`
}

func (t *tokens) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// newTokens açılışta yolun yanındaki artık `.state-*.tmp` dosyalarını siler
// (Write ile Rename arasındaki çökme canlı bir jeton çifti bırakabilir), sonra
// durumu yükler. Yükleme hatası "bağlı değil" demektir.
func newTokens(path, clientID string, c *client, now func() time.Time) (*tokens, error) {
	if err := sweepStrayTemps(filepath.Dir(path)); err != nil {
		return nil, err
	}
	t := &tokens{path: path, clientID: clientID, c: c, now: now}
	st, ok, err := loadState(path, clientID)
	if err != nil {
		slog.Warn("github state file unusable, treating as not connected")
		return t, nil
	}
	t.st, t.ok = st, ok
	return t, nil
}

// connectedLocked bağlı olup olmadığını söyler (mu tutulurken çağrılır).
func (t *tokens) connectedLocked() bool {
	return t.ok && t.st.ClientID == t.clientID
}

// ReconnectRequired, ErrReconnect yüzünden bağlantı düştüyse true döner;
// yeni bağlantı kaydedilene kadar true kalır.
func (t *tokens) ReconnectRequired() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.reconnect
}

// dropForReconnect ölü kimlik bilgilerini bellekten ve diskten kaldırır
// (diskteki silme en iyi gayretle) ve ErrReconnect döner. Günlük metni sabittir.
func (t *tokens) dropForReconnect() error {
	t.st = state{}
	t.ok = false
	t.reconnect = true
	if t.path != "" {
		if err := removeState(t.path); err != nil {
			slog.Warn("github state file could not be removed")
		}
	}
	slog.Warn("github connection lost: reconnect required")
	return ErrReconnect
}

// Token geçerli erişim jetonunu döner; bitimine 5 dakikadan az kaldıysa önce
// yeniler. Yenileme geçici olarak başarısız olursa ve jeton henüz geçerliyse
// eskisi hatasız döner.
func (t *tokens) Token(ctx context.Context) (string, error) {
	// Bekleyenler kendi ctx değerlerinden bağımsız mu üzerinde bekler: tutan taraf istemci zaman aşımıyla sınırlıdır.
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.connectedLocked() {
		return "", ErrNotConnected
	}
	now := t.clock()
	exp := t.st.AccessExpiresAt
	if !exp.IsZero() && exp.Sub(now) < refreshMargin {
		if err := t.refreshLocked(ctx); err != nil {
			if errors.Is(err, ErrReconnect) || !now.Before(exp) {
				return "", err
			}
		}
	}
	return t.st.AccessToken, nil
}

// ForceRefresh, sunucunun reddettiği (rejected) jetonu yeniler. Bu arada başka
// bir çağıran yenilediyse (jeton rejected değilse) istek atmadan güncel jetonu döner.
func (t *tokens) ForceRefresh(ctx context.Context, rejected string) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.connectedLocked() {
		return "", ErrNotConnected
	}
	if t.st.AccessToken != rejected {
		return t.st.AccessToken, nil
	}
	if err := t.refreshLocked(ctx); err != nil {
		return "", err
	}
	return t.st.AccessToken, nil
}

// refreshLocked mu tutulurken çağrılır. Yeni çift diske yazılmadan kullanılmaz:
// yenileme jetonu tek kullanımlıktır, eskisi artık ölüdür.
func (t *tokens) refreshLocked(ctx context.Context) error {
	now := t.clock()
	rexp := t.st.RefreshExpiresAt
	if t.st.RefreshToken == "" || (!rexp.IsZero() && !rexp.After(now)) {
		return t.dropForReconnect()
	}
	out, err := t.requestRefresh(ctx)
	if err != nil {
		return err
	}
	next := t.st
	next.AccessToken, next.RefreshToken = out.AccessToken, out.RefreshToken
	next.AccessExpiresAt, next.RefreshExpiresAt = expiry(now, out.ExpiresIn), expiry(now, out.RefreshExpiresIn)
	if err := saveStateFn(t.path, next); err != nil {
		slog.Error("github: could not persist refreshed token", "err", err)
		return t.dropForReconnect()
	}
	t.st = next
	t.reconnect = false
	return nil
}

// requestRefresh yenileme isteğini yollar ve yanıtı doğrular. Geçici hatalar
// sarılıp döner (durum korunur); 401 ve her OAuth hata kodu kalıcıdır (ErrReconnect).
func (t *tokens) requestRefresh(ctx context.Context) (refreshResponse, error) {
	form := url.Values{
		"client_id":     {t.clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.st.RefreshToken},
	}
	var out refreshResponse
	if err := t.c.postForm(ctx, refreshPath, form, &out); err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return refreshResponse{}, t.dropForReconnect()
		}
		return refreshResponse{}, fmt.Errorf("github: token refresh failed: %w", err)
	}
	if out.Error != "" {
		return refreshResponse{}, t.dropForReconnect()
	}
	if out.AccessToken == "" || out.RefreshToken == "" {
		return refreshResponse{}, fmt.Errorf("github: token refresh failed: %w", errResponse)
	}
	return out, nil
}

// expiry now + saniye; sıfır ya da eksik değer süresiz (sıfır zaman) demektir.
func expiry(now time.Time, seconds int64) time.Time {
	if seconds <= 0 {
		return time.Time{}
	}
	if seconds > int64(maxExpiry/time.Second) {
		return now.Add(maxExpiry)
	}
	return now.Add(time.Duration(seconds) * time.Second)
}

package github

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"sync"
	"time"
)

const (
	refreshPath = "/login/oauth/access_token"
	// refreshMargin: erişim jetonunun bitimine bundan az kaldıysa önce yenilenir.
	refreshMargin = 5 * time.Minute
)

var (
	ErrNotConnected = errors.New("github: not connected")
	ErrReconnect    = errors.New("github: connection lost: reconnect GitHub")

	errRefreshRejected = errors.New("github: token refresh rejected")
)

// saveStateFn testlerin kaydetme hatasını zorlayabilmesi için bir kancadır.
var saveStateFn = saveState

// deadRefreshErrors GitHub'ın 200 ile döndüğü, yenilemenin kalıcı olarak
// bittiği OAuth hata kodlarıdır; kullanıcının yeniden bağlanması gerekir.
var deadRefreshErrors = map[string]bool{
	"bad_refresh_token":            true,
	"incorrect_client_credentials": true,
	"unauthorized_client":          true,
}

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

// connectedLocked bağlı olup olmadığını söyler (mu tutulurken çağrılır).
func (t *tokens) connectedLocked() bool {
	return t.ok && t.st.ClientID == t.clientID
}

// drop bellekteki durumu (ve jetonları) bırakır; dosyaya dokunmaz.
func (t *tokens) drop() {
	t.st = state{}
	t.ok = false
}

// Token geçerli erişim jetonunu döner; bitimine 5 dakikadan az kaldıysa önce yeniler.
func (t *tokens) Token(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.connectedLocked() {
		return "", ErrNotConnected
	}
	exp := t.st.AccessExpiresAt
	if !exp.IsZero() && exp.Sub(t.clock()) < refreshMargin {
		if err := t.refreshLocked(ctx); err != nil {
			return "", err
		}
	}
	return t.st.AccessToken, nil
}

// ForceRefresh jetonu süresine bakmadan yeniler (401 sonrası).
func (t *tokens) ForceRefresh(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.connectedLocked() {
		return "", ErrNotConnected
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
		t.drop()
		return ErrReconnect
	}
	out, err := t.requestRefresh(ctx)
	if err != nil {
		return err
	}
	next := t.st
	next.AccessToken, next.RefreshToken = out.AccessToken, out.RefreshToken
	next.AccessExpiresAt, next.RefreshExpiresAt = expiry(now, out.ExpiresIn), expiry(now, out.RefreshExpiresIn)
	if err := saveStateFn(t.path, next); err != nil {
		slog.Error("github: could not persist refreshed token, connection dropped", "err", err)
		t.drop()
		return ErrReconnect
	}
	t.st = next
	return nil
}

// requestRefresh yenileme isteğini yollar ve yanıtı doğrular. Geçici hatalar
// sarılıp döner (durum korunur); kalıcı OAuth hataları ErrReconnect olur.
func (t *tokens) requestRefresh(ctx context.Context) (refreshResponse, error) {
	form := url.Values{
		"client_id":     {t.clientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {t.st.RefreshToken},
	}
	var out refreshResponse
	if err := t.c.postForm(ctx, refreshPath, form, &out); err != nil {
		return refreshResponse{}, fmt.Errorf("github: token refresh failed: %w", err)
	}
	if out.Error != "" {
		if deadRefreshErrors[out.Error] {
			t.drop()
			return refreshResponse{}, ErrReconnect
		}
		return refreshResponse{}, errRefreshRejected
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
	return now.Add(time.Duration(seconds) * time.Second)
}

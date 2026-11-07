// Package wire, masaüstünün dağıtıcısını CLI ile AYNI dosyalara bağlar:
// sunucu listesi (servers.json, K-162) ve TOTP oturumları (sessions/,
// K-155). Masaüstü açıkken CLI'da yapılan `kadran login` ya da
// `kadran server add` bir sonraki çağrıda görülür.
package wire

import (
	"context"
	"strings"
	"time"

	"github.com/erkanrzgc/kadran/internal/client"
	"github.com/erkanrzgc/kadran/internal/gitref"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
	"github.com/erkanrzgc/kadran/internal/servers"
	"github.com/erkanrzgc/kadran/internal/sidecar"
)

// CallTimeout, tek bir okuma çağrısının süresi; `kadran sidecar`'ın
// varsayılanıyla aynı. Yönetim yöntemleri kendi sürelerini kullanır.
const CallTimeout = 30 * time.Second

// Stores, CLI ile paylaşılan dosyalar. Boş bırakılan alan CLI'ın
// varsayılan yerini kullanır (os.UserConfigDir()/kadran).
type Stores struct {
	Sessions *client.SessionStore
	Servers  servers.Store
}

// writer, dal çözümünün ilerleme satırını uyarı günlüğüne verir.
type writer func(string)

func (w writer) Write(p []byte) (int, error) {
	if line := strings.TrimSpace(string(p)); line != "" {
		w(line)
	}
	return len(p), nil
}

// LoadSession, hedefin kayıtlı oturum jetonu; CLI'ın oturumuyla aynı
// anahtar ("srv" ile "kadran-client@srv" aynı oturumu paylaşır).
func (s Stores) LoadSession(rawTarget string) (string, error) {
	t, err := client.ParseTarget(rawTarget)
	if err != nil {
		return "", err
	}
	var store client.SessionStore
	if s.Sessions != nil {
		store = *s.Sessions
	} else if store, err = client.DefaultSessionStore(); err != nil {
		return "", err
	}
	tok, _, err := store.Load(t.String(), time.Now())
	return tok, err
}

// Dispatcher, masaüstünün süreç içi dağıtıcısı.
func Dispatcher(stores Stores, warn func(string)) *sidecar.Dispatcher {
	if warn == nil {
		warn = func(string) {}
	}
	return sidecar.New(sidecar.Deps{
		CallTimeout: CallTimeout,
		Servers:     stores.Servers,
		LoadSession: stores.LoadSession,
		ResolveCommit: func(ctx context.Context, rpc kadranv1.KadranServiceClient, appID, commit, branch string) (string, error) {
			return gitref.ResolveCommit(ctx, rpc, appID, commit, branch, writer(warn))
		},
		Warn: warn,
	})
}

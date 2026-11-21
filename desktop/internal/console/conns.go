package console

import (
	"context"
	"fmt"
	"sync"

	"github.com/erkanrzgc/kadran/internal/client"
	kadranv1 "github.com/erkanrzgc/kadran/internal/pb/kadran/v1"
)

// Conns, konsolun sunucu başına tuttuğu kalıcı bağlantıları yönetir.
// Bağlantı önbelleğe alınır; ping alınmaz: TotpRequired bir konsol oturumu
// içinde değişebildiği için (totp enable/disable/reset) her çağrıda yeniden
// ping atılır.
type Conns struct {
	dial func(client.Target) (*client.Client, error)

	mu    sync.Mutex
	conns map[string]*client.Client
}

// NewConns, verilen bağlantı kurucuyla bir önbellek kurar. Üretimde kurucu
// client.DialWith(t, client.DialOptions{HideConsole: true}) olur.
func NewConns(dial func(client.Target) (*client.Client, error)) *Conns {
	return &Conns{dial: dial, conns: map[string]*client.Client{}}
}

// Dial, hedef için önbellekteki bağlantıyı (yoksa yenisini) alır, her
// çağrıda CheckProtocol ile yeniden ping atar ve Close'u etkisiz bir paylaşılan
// tutamaç döner. Ping başarısız olursa bağlantı kapatılır, bir kez yeniden
// bağlanılır; o da başarısızsa hata döner ve hiçbir şey önbellekte kalmaz.
func (c *Conns) Dial(ctx context.Context, rawTarget string) (*client.Client, *kadranv1.PingResponse, error) {
	target, err := client.ParseTarget(rawTarget)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if owner, ok := c.conns[rawTarget]; ok {
		if ping, err := owner.CheckProtocol(ctx); err == nil {
			return owner.Shared(), ping, nil
		}
		c.drop(rawTarget)
	}
	owner, err := c.dial(target)
	if err != nil {
		return nil, nil, err
	}
	ping, err := owner.CheckProtocol(ctx)
	if err != nil {
		_ = owner.Close()
		return nil, nil, fmt.Errorf("console: %w", err)
	}
	c.conns[rawTarget] = owner
	return owner.Shared(), ping, nil
}

// drop, önbellekteki bağlantıyı kapatıp siler. Çağıran mu'yu tutmalı.
func (c *Conns) drop(rawTarget string) {
	if owner, ok := c.conns[rawTarget]; ok {
		_ = owner.Close()
		delete(c.conns, rawTarget)
	}
}

// Close, tüm bağlantıları kapatır.
func (c *Conns) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.conns {
		c.drop(k)
	}
}

package deploy

import (
	"context"
	"sync"
)

// Coordinator serializes the entire lifecycle of each app, including builds
// and delayed drain, while allowing unrelated apps to proceed independently.
// Acquire only at API/supervisor boundaries; rollout methods never acquire it.
// Its zero value is ready for use.
type Coordinator struct {
	mu   sync.Mutex
	apps map[string][]*appWaiter
}

type appWaiter struct{ ready chan struct{} }

func NewCoordinator() *Coordinator { return &Coordinator{} }

// Acquire joins the app's FIFO queue. Canceled waiters are removed and cannot
// execute work, including cancellation racing with ownership handoff.
// The returned release must be called after the complete operation finishes.
func (c *Coordinator) Acquire(ctx context.Context, appID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w := &appWaiter{ready: make(chan struct{})}
	c.mu.Lock()
	if c.apps == nil {
		c.apps = make(map[string][]*appWaiter)
	}
	queue := c.apps[appID]
	c.apps[appID] = append(queue, w)
	if len(queue) == 0 {
		close(w.ready)
	}
	c.mu.Unlock()

	select {
	case <-ctx.Done():
		c.remove(appID, w)
		return nil, ctx.Err()
	case <-w.ready:
		if err := ctx.Err(); err != nil {
			c.remove(appID, w)
			return nil, err
		}
		var once sync.Once
		return func() { once.Do(func() { c.remove(appID, w) }) }, nil
	}
}

func (c *Coordinator) remove(appID string, w *appWaiter) {
	c.mu.Lock()
	defer c.mu.Unlock()
	queue := c.apps[appID]
	for i, candidate := range queue {
		if candidate != w {
			continue
		}
		queue = append(queue[:i], queue[i+1:]...)
		if len(queue) == 0 {
			delete(c.apps, appID)
		} else {
			c.apps[appID] = queue
			if i == 0 {
				close(queue[0].ready)
			}
		}
		return
	}
}

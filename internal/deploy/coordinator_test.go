package deploy

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitForQueue(t *testing.T, c *Coordinator, app string, n int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		got := len(c.apps[app])
		c.mu.Unlock()
		if got == n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("app %s did not reach queue length %d", app, n)
}

func TestCoordinatorFIFOAndCanceledWaiter(t *testing.T) {
	c := NewCoordinator()
	owner, err := c.Acquire(context.Background(), "blog")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	canceled := make(chan error, 1)
	go func() { _, err := c.Acquire(ctx, "blog"); canceled <- err }()
	waitForQueue(t, c, "blog", 2)
	order := make(chan int, 2)
	for i := 1; i <= 2; i++ {
		go func(id int) {
			release, err := c.Acquire(context.Background(), "blog")
			if err != nil {
				return
			}
			order <- id
			release()
		}(i)
		waitForQueue(t, c, "blog", i+2)
	}
	cancel()
	if err := <-canceled; !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled wait = %v", err)
	}
	owner()
	if first, second := <-order, <-order; first != 1 || second != 2 {
		t.Fatalf("queue order = %d,%d", first, second)
	}
	waitForQueue(t, c, "blog", 0)
}

func TestCoordinatorOtherAppsProceedAndReleaseIsIdempotent(t *testing.T) {
	c := NewCoordinator()
	blog, _ := c.Acquire(context.Background(), "blog")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	shop, err := c.Acquire(ctx, "shop")
	if err != nil {
		t.Fatalf("unrelated app blocked: %v", err)
	}
	shop()
	shop()
	blog()
	blog()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.apps) != 0 {
		t.Fatalf("idle app queues leaked: %v", c.apps)
	}
}

func TestCoordinatorCancellationHandoffRace(t *testing.T) {
	c := NewCoordinator()
	for range 100 {
		owner, _ := c.Acquire(context.Background(), "blog")
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			release, err := c.Acquire(ctx, "blog")
			if release != nil {
				release()
			}
			done <- err
		}()
		waitForQueue(t, c, "blog", 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); cancel() }()
		go func() { defer wg.Done(); owner() }()
		wg.Wait()
		<-done
		ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
		next, err := c.Acquire(ctx2, "blog")
		cancel2()
		if err != nil {
			t.Fatalf("handoff left an app locked: %v", err)
		}
		next()
	}
}

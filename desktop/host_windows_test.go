package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/erkanrzgc/kadran/desktop/internal/confirm"
	"github.com/erkanrzgc/kadran/desktop/internal/win"
)

// Her yerel kutu (GUI düğmeleri, konsol satırı, konsol ayarı) host.Confirm'den
// geçer: biri açıkken ikinci çağrı kutu göstermeden ErrBoxOpen alır; kutu
// kapanınca sonraki gösterilir.
func TestHostConfirmShowsOneBoxAtATime(t *testing.T) {
	var shownCount atomic.Int32
	hold, shown := make(chan struct{}), make(chan struct{}, 4)
	h := &host{warn: func(string) {}, showBox: func(windows.HWND, win.Box) (bool, error) {
		shownCount.Add(1)
		shown <- struct{}{}
		<-hold
		return true, nil
	}}
	first := make(chan error, 1)
	go func() {
		_, err := h.Confirm(confirm.Box{Title: "gui"})
		first <- err
	}()
	select {
	case <-shown:
	case <-time.After(5 * time.Second):
		t.Fatal("ilk kutu gösterilmedi")
	}

	second := make(chan error, 1)
	go func() {
		ok, err := h.Confirm(confirm.Box{Title: "console"})
		if ok {
			err = errors.New("ikinci kutu onaylandı")
		}
		second <- err
	}()
	select {
	case err := <-second:
		if !errors.Is(err, confirm.ErrBoxOpen) {
			t.Fatalf("ikinci kutu: %v", err)
		}
	case <-time.After(5 * time.Second):
		close(hold)
		t.Fatalf("ikinci çağrı beklemeye düştü; gösterilen kutu %d", shownCount.Load())
	}
	if n := shownCount.Load(); n != 1 {
		t.Fatalf("%d kutu gösterildi, 1 olmalı", n)
	}

	close(hold)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if ok, err := h.Confirm(confirm.Box{Title: "later"}); !ok || err != nil || shownCount.Load() != 2 {
		t.Fatalf("kutu kapanınca yenisi açılmadı: ok %v err %v gösterilen %d", ok, err, shownCount.Load())
	}
}

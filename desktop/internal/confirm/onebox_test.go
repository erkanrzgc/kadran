package confirm

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// İki eşzamanlı kutu isteği: yalnız biri gösterilir, ikincisi kutu açmadan
// ErrBoxOpen ile reddedilir; ilk kutu kapanınca kapı yeniden açılır.
func TestOneBoxShowsOneAtATime(t *testing.T) {
	var gate OneBox
	var shownCount atomic.Int32
	hold, shown := make(chan struct{}), make(chan struct{}, 1)
	first := make(chan error, 1)
	go func() {
		ok, err := gate.Show(func() (bool, error) {
			shownCount.Add(1)
			shown <- struct{}{}
			<-hold
			return true, nil
		})
		if !ok && err == nil {
			err = errors.New("ilk kutu onaylanmadı")
		}
		first <- err
	}()
	select {
	case <-shown:
	case <-time.After(5 * time.Second):
		t.Fatal("ilk kutu gösterilmedi")
	}

	ok, err := gate.Show(func() (bool, error) {
		shownCount.Add(1)
		return true, nil
	})
	if ok || !errors.Is(err, ErrBoxOpen) {
		t.Fatalf("ikinci kutu: ok %v err %v", ok, err)
	}
	if n := shownCount.Load(); n != 1 {
		t.Fatalf("%d kutu gösterildi, 1 olmalı", n)
	}

	close(hold)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	ok, err = gate.Show(func() (bool, error) {
		shownCount.Add(1)
		return true, nil
	})
	if !ok || err != nil || shownCount.Load() != 2 {
		t.Fatalf("kutu kapanınca kapı açılmadı: ok %v err %v gösterilen %d", ok, err, shownCount.Load())
	}
}

// Kutunun hatası ve paniği kapıyı kilitli bırakmaz.
func TestOneBoxReleasesAfterErrorAndPanic(t *testing.T) {
	var gate OneBox
	boom := errors.New("dialog failed")
	if ok, err := gate.Show(func() (bool, error) { return true, boom }); !errors.Is(err, boom) || ok {
		t.Fatalf("hata geçmedi: ok %v err %v", ok, err)
	}
	func() {
		defer func() { _ = recover() }()
		_, _ = gate.Show(func() (bool, error) { panic("boom") })
	}()
	if ok, err := gate.Show(func() (bool, error) { return true, nil }); !ok || err != nil {
		t.Fatalf("kapı kilitli kaldı: ok %v err %v", ok, err)
	}
}

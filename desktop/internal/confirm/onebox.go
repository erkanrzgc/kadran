package confirm

import (
	"errors"
	"sync/atomic"
)

// ErrBoxOpen: başka bir yerel onay kutusu zaten açık; ikincisi gösterilmedi.
var ErrBoxOpen = errors.New("another confirmation is open; answer it first")

// OneBox, aynı anda tek yerel onay kutusu kapısı. GUI düğmeleri, konsol ve
// konsol ayarı aynı kapıdan geçer: iki kipli görev kutusu aynı pencereye
// bağlanırsa ilki kapanınca Windows pencereyi ikincisi açıkken yeniden
// etkinleştirir. Kapı beklemez: kutu açıkken gelen istek kutu göstermeden
// (false, ErrBoxOpen) alır (kapalı başarısızlık). Sıfır değeri kullanıma hazır.
type OneBox struct{ open atomic.Bool }

// Show, kapı boşsa show'u çağırır ve sonucunu döner; hata dönen kutu onay
// sayılmaz. Kutu kapanınca (hata ya da panikte de) kapı açılır.
func (o *OneBox) Show(show func() (bool, error)) (bool, error) {
	if !o.open.CompareAndSwap(false, true) {
		return false, ErrBoxOpen
	}
	defer o.open.Store(false)
	ok, err := show()
	if err != nil {
		return false, err
	}
	return ok, nil
}

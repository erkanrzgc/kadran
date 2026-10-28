package totp

import "time"

// Kaba kuvvet kilidi.
//
// Her deneme yeni bir SSH bağlantısı; sınır olmasaydı 6 hanelik kod
// dakikalar içinde taranırdı. 5 ardışık başarısızlıktan sonra kilit 1
// dakika, sonraki her başarısızlıkta ikiye katlanır, en çok 1 saat: bir
// anahtar saatte en fazla ~10 kod deneyebilir.
const (
	LockAfter = 5
	LockBase  = time.Minute
	LockMax   = time.Hour
)

// LockDuration, bu kadar ardışık başarısızlıktan sonraki kilit süresidir.
func LockDuration(failures int) time.Duration {
	if failures < LockAfter {
		return 0
	}
	d := LockBase
	for i := LockAfter; i < failures && d < LockMax; i++ {
		d *= 2
	}
	if d > LockMax {
		d = LockMax
	}
	return d
}

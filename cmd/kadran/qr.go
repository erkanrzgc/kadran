package main

import (
	"strings"

	"rsc.io/qr"
)

// qrQuiet, QR'ın çevresindeki açık sessiz bölgedir (modül). Standart 4
// istiyor; okuyucular kodun nerede başladığını buradan buluyor.
const qrQuiet = 4

// qrBlock, metni terminalde taranabilir bir QR koda çevirir (K-156).
//
// Yarım blok karakterleri: her satır iki modül satırı taşır, böylece modüller
// kabaca kare görünür. AÇIK modüller ve sessiz bölge blok olarak, koyular
// boşluk olarak çiziliyor: koyu zeminli terminal varsayımı (Windows Terminal,
// PowerShell ve çoğu terminalin varsayılanı). Açık zeminli bir terminalde
// renkler ters döner; o yüzden çağıran anahtarı da yazıyor. Renk kaçış kodu
// kullanılmıyor: her Windows konsolu onları işlemiyor.
func qrBlock(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}
	n := code.Size + 2*qrQuiet
	light := func(x, y int) bool {
		if y >= n {
			return false // tek sayıdaki son satırın alt yarısı: terminal zemini
		}
		x, y = x-qrQuiet, y-qrQuiet
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return true
		}
		return !code.Black(x, y)
	}
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			top, bottom := light(x, y), light(x, y+1)
			switch {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteByte(' ')
			}
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

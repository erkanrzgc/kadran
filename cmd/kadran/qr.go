package main

import (
	"net/url"
	"strings"

	"rsc.io/qr"
)

// qrQuiet, QR'ın çevresindeki sessiz bölgedir (modül). Standart 4 istiyor;
// okuyucular kodun nerede başladığını buradan buluyor.
const qrQuiet = 4

// qrBlock, metni terminalde taranabilir bir QR koda çevirir (K-156).
//
// Yarım blok karakterleri: her satır iki modül satırı taşır, böylece modüller
// kabaca kare görünür. KOYU modüller blok, açıklar ve sessiz bölge boşluk:
// koyu zeminli terminalde (Windows Terminal, PowerShell ve çoğu terminalin
// varsayılanı) kod ters renkli, koyu zemin üstünde açık kareler olarak
// görünüyor, etrafında beyaz kutu yok. Açık zeminli terminalde normal renkli
// çıkıyor. Ters kodu okumayan uygulama için çağıran anahtarı da yazıyor.
// Hata düzeltme L: ekrandaki kod hasar görmüyor, kod küçülüyor. Renk kaçış
// kodu kullanılmıyor: her Windows konsolu onları işlemiyor.
func qrBlock(text string) (string, error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return "", err
	}
	n := code.Size + 2*qrQuiet
	// Black kodun dışında (sessiz bölge, tek sayıdaki son satırın alt
	// yarısı) false döndürüyor: oralar boşluk, yani terminal zemini.
	dark := func(x, y int) bool { return code.Black(x-qrQuiet, y-qrQuiet) }
	var b strings.Builder
	for y := 0; y < n; y += 2 {
		for x := 0; x < n; x++ {
			top, bottom := dark(x, y), dark(x, y+1)
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

// qrDefaults, Key URI biçiminin varsayılanlarıdır; uygulama bu parametreler
// yokken de böyle varsayıyor.
var qrDefaults = map[string]string{"algorithm": "SHA1", "digits": "6", "period": "30"}

// qrLink, QR'a girecek bağlantıdır: varsayılanına eşit parametreler atılıyor.
// Bağlantı kısalınca QR bir sürüm küçülüyor (24 karakterlik ana bilgisayar
// adıyla 41'den 37 modüle). Ekrana yazılan link sunucununki, tam kalıyor.
func qrLink(uri string) string {
	base, query, ok := strings.Cut(uri, "?")
	if !ok {
		return uri
	}
	q, err := url.ParseQuery(query)
	if err != nil {
		return uri
	}
	for k, v := range qrDefaults {
		if len(q[k]) == 1 && q[k][0] == v {
			q.Del(k)
		}
	}
	return base + "?" + q.Encode()
}

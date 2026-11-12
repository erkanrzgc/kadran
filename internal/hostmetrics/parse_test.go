package hostmetrics

import (
	"strings"
	"testing"
)

// Uzun arayüz adında çekirdek ad ile sayacın arasına boşluk koymuyor.
func TestNetDevNameGluedToCounters(t *testing.T) {
	in := "h1\nh2\neth1:7000 1 0 0 0 0 0 0 8000 1 0 0 0 0 0 0\n"
	rx, tx, ok := parseNetDev(strings.NewReader(in))
	if !ok || rx != 7000 || tx != 8000 {
		t.Fatalf("parseNetDev = %d, %d, %v", rx, tx, ok)
	}
}

func TestNetDevOnlyVirtualIsMeasuredAsZero(t *testing.T) {
	in := "h1\nh2\n  lo: 5 1 0 0 0 0 0 0 5 1 0 0 0 0 0 0\n"
	rx, tx, ok := parseNetDev(strings.NewReader(in))
	if !ok || rx != 0 || tx != 0 {
		t.Fatalf("yalnız lo: %d, %d, %v (okundu ama fiziksel arayüz yok = 0)", rx, tx, ok)
	}
}

func TestStatWithoutCPULine(t *testing.T) {
	if _, ok := parseStat(strings.NewReader("intr 1\nctxt 2\n")); ok {
		t.Fatal("cpu satırı yokken ok dönmemeli")
	}
	if _, ok := parseStat(strings.NewReader("cpu  1 2 x 4\n")); ok {
		t.Fatal("bozuk sayı kabul edildi")
	}
}

func TestPressureNeedsSomeLine(t *testing.T) {
	if _, ok := parsePressure(strings.NewReader("full avg10=1.00 avg60=0 avg300=0 total=0\n")); ok {
		t.Fatal("some satırı yokken ok dönmemeli")
	}
	v, ok := parsePressure(strings.NewReader("some avg10=12.34 avg60=0 avg300=0 total=0\n"))
	if !ok || v != 12.34 {
		t.Fatalf("parsePressure = %v, %v", v, ok)
	}
}

func TestLoadavgRejectsGarbage(t *testing.T) {
	if _, _, _, _, ok := parseLoadavg("0.1 0.2\n"); ok {
		t.Fatal("eksik alan kabul edildi")
	}
	if _, _, _, _, ok := parseLoadavg("0.1 0.2 0.3 12 99\n"); ok {
		t.Fatal("'çalışan/toplam' biçimi olmayan görev alanı kabul edildi")
	}
}

func TestDiskstatsUsesPredicate(t *testing.T) {
	in := " 8 0 sda 1 0 10 0 1 0 20 0 0 0 0\n 8 1 sda1 1 0 99 0 1 0 99 0 0 0 0\n"
	r, w, ok := parseDiskstats(strings.NewReader(in), func(name string) bool { return name == "sda" })
	if !ok || r != 10*512 || w != 20*512 {
		t.Fatalf("parseDiskstats = %d, %d, %v", r, w, ok)
	}
}

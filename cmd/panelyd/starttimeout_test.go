package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/erkanrzgc/panely/internal/execclient"
)

// ── Açılış systemd'nin sınırına sığmalı (K-117) ──────────────────────
//
// READY'den önce panelyd executor'ı yokluyor ve ters vekili SQLite'tan
// uzlaştırıyor; ikisinin de süre sınırı var ve toplamları systemd'nin
// varsayılan TimeoutStartSec'ini (90 sn) aşabiliyor. Aşarsa panelyd tam
// READY göndermek üzereyken öldürülür, Restart=on-failure onu aynı duvara
// geri gönderir ve `panely status` hiç cevap vermez. Açılış yolundaki
// "ölümcül değil, görünür kıl" tasarımının tam tersi.
//
// Watchdog bu süreyi KAPSAMIYOR: systemd onu READY'den sonra kuruyor.

// startupMargin, sayılmayan açılış işleri için pay: store.Open (göç
// öncesi yedek dahil), grup çözümü, soket kurulumu. TAHMİN; küçük bir
// veritabanında saniyenin altında.
const startupMargin = 30 * time.Second

// worstStartupBeforeReady, koddaki sınırlardan türetilen en kötü açılış:
// executor yoklaması + her deneme tam sınırını dolduran uzlaştırma +
// denemeler arası beklemeler (reconcileAtStartup: 1 sn, 2 sn, …).
func worstStartupBeforeReady() time.Duration {
	var sleeps time.Duration
	for a := 1; a < startupReconcileTries; a++ {
		sleeps += time.Duration(a) * time.Second
	}
	return execclient.DefaultTimeout +
		time.Duration(startupReconcileTries)*startupReconcileTimeout + sleeps
}

// Sayı SABİTTEN okunmuyor: K-117 ve birimdeki yorum 103 sn diyor. Sabitler
// değişirse bu test ikisinin de güncellenmesi gerektiğini söyler.
func TestWorstStartupIsWhatK117Says(t *testing.T) {
	if got := worstStartupBeforeReady(); got != 103*time.Second {
		t.Fatalf("en kötü açılış %s — K-117'yi, birim yorumunu ve TimeoutStartSec'i güncelle", got)
	}
}

func TestUnitStartTimeoutCoversTheWorstStartup(t *testing.T) {
	yol := filepath.Join("..", "..", "deploy", "systemd", "panelyd.service")
	f, err := os.Open(yol)
	if err != nil {
		t.Fatalf("birim okunamadı: %v", err)
	}
	defer func() { _ = f.Close() }()

	var degerler []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		satir := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(satir, "#") {
			continue
		}
		if ad, deger, ok := strings.Cut(satir, "="); ok && strings.TrimSpace(ad) == "TimeoutStartSec" {
			degerler = append(degerler, strings.TrimSpace(deger))
		}
	}
	if len(degerler) != 1 {
		t.Fatalf("TimeoutStartSec= %v — varsayılan 90 sn, en kötü açılış %s",
			degerler, worstStartupBeforeReady())
	}
	sinir, err := time.ParseDuration(degerler[0])
	if err != nil {
		t.Fatalf("TimeoutStartSec=%s çözümlenemedi (Go süre biçiminde yazılmalı, ör. 180s): %v",
			degerler[0], err)
	}
	if gerek := worstStartupBeforeReady() + startupMargin; sinir < gerek {
		t.Errorf("TimeoutStartSec=%s < en kötü açılış %s + pay %s — systemd panelyd'yi READY'den önce öldürür",
			sinir, worstStartupBeforeReady(), startupMargin)
	}
}

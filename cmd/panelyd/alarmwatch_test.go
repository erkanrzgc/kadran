package main

import (
	"testing"

	"github.com/erkanrzgc/panely/internal/store"
)

// TestDiskDecisionThresholds, eşikleri TABLO HÂLİNDE sınar.
func TestDiskDecisionThresholds(t *testing.T) {
	cases := []struct {
		name     string
		free     float64
		action   diskAction
		severity string
	}{
		{"disk bitti", 0.00, diskRaise, store.SeverityCritical},
		{"kritik eşiğin altı", 0.04, diskRaise, store.SeverityCritical},
		{"tam kritik eşik", 0.05, diskRaise, store.SeverityWarning},
		{"uyarı bölgesi", 0.10, diskRaise, store.SeverityWarning},
		{"tam uyarı eşiği", 0.15, diskHold, ""},
		{"histerezis bandı", 0.18, diskHold, ""},
		{"tam kapanma eşiği", 0.20, diskClear, ""},
		{"bol yer", 0.84, diskClear, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			action, severity := diskDecision(tc.free)
			if action != tc.action {
				t.Errorf("%.2f boş → eylem %v, %v bekleniyordu",
					tc.free, action, tc.action)
			}
			if severity != tc.severity {
				t.Errorf("%.2f boş → ciddiyet %q, %q bekleniyordu",
					tc.free, severity, tc.severity)
			}
		})
	}
}

// TestDiskHysteresisHasGap, açılma ve kapanma eşiklerinin AYRI olduğunu
// doğrular.
//
// ── Bu testin asıl iddiası ──────────────────────────────────────────
//
// Tek eşik olsaydı, tam sınırın etrafında salınan bir disk her turda
// alarm açıp kapatırdı. Bu projenin kendi kuralı: "yanlış alarm veren
// bir kontrol kapatılmaya mahkûmdur" ve "tekrarlayan sahte alarmların
// sonu, gerçek olanın da yok sayılmasıdır."
//
// Eşitlik iddiası doğrudan sınanıyor: birisi sabitleri eşitlerse bu
// test kırmızıya döner. Yalnızca tablo testi olsaydı, iki sabiti de
// aynı değere çeken bir değişiklik tabloyu güncelleyerek sessizce
// geçebilirdi.
func TestDiskHysteresisHasGap(t *testing.T) {
	if diskClearFree <= diskWarnFree {
		t.Fatalf("kapanma eşiği (%.2f) açılma eşiğinden (%.2f) yüksek "+
			"DEĞİL — sınırda salınan disk her turda alarm açıp kapatır",
			diskClearFree, diskWarnFree)
	}
	if diskCriticalFree >= diskWarnFree {
		t.Errorf("kritik eşik (%.2f) uyarı eşiğinden (%.2f) düşük olmalı",
			diskCriticalFree, diskWarnFree)
	}

	// Bandın içinde HİÇBİR ölçüm durum değiştirmemeli.
	for free := diskWarnFree; free < diskClearFree; free += 0.01 {
		if action, _ := diskDecision(free); action != diskHold {
			t.Errorf("%.2f histerezis bandında ama eylem %v", free, action)
		}
	}
}

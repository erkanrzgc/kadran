package main

import (
	"strings"
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

// TestDiskAlarmIDMatchesClearID, YÜKSELTME ve KAPATMA yollarının aynı
// kimliği kullandığını doğrular.
//
// ── Neden bu test var ───────────────────────────────────────────────
//
// `checkDisk` alarmı `diskAlarm()` ile AÇIYOR ve `diskAlarmID` ile
// KAPATIYOR. İkisi ayrışsaydı alarm açılır ama bir daha asla
// kapanmazdı — ve kapanmayan bir alarm, bakılmayan bir alarma dönüşür.
//
// Hiçbir mevcut test bunu göremezdi: her iki taraf da aynı sabitten
// besleniyor, dolayısıyla sapma ancak biri elle değiştirilirse doğar
// ve o an sessizce geçerdi.
func TestDiskAlarmIDMatchesClearID(t *testing.T) {
	raised := diskAlarm(store.SeverityWarning, 0.10, 100, 1000)
	if raised.ID != diskAlarmID {
		t.Errorf("yükseltilen alarmın kimliği %q, kapatma %q kullanıyor — "+
			"alarm açılır ama ASLA kapanmaz", raised.ID, diskAlarmID)
	}
	if raised.Kind == "" || raised.Target == "" {
		t.Errorf("alarm türü/hedefi boş: %+v", raised)
	}
	// Ayrıntı ölçümü TAŞIMALI: "disk az" demek operatöre ne kadar
	// kaldığını söylemez.
	if !strings.Contains(raised.Detail, "%") {
		t.Errorf("ayrıntı oran taşımıyor: %q", raised.Detail)
	}
}

// TestHumanBytesFormatsSizes, ayrıntıdaki boyutların okunur olduğunu
// doğrular.
func TestHumanBytesFormatsSizes(t *testing.T) {
	cases := map[uint64]string{
		512:                     "512 B",
		1024:                    "1.0 KiB",
		1536:                    "1.5 KiB",
		1024 * 1024:             "1.0 MiB",
		38 * 1024 * 1024 * 1024: "38.0 GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, %q bekleniyordu", in, got, want)
		}
	}
}

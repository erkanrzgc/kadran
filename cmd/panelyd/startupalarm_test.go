package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/erkanrzgc/panely/internal/alarm"
	"github.com/erkanrzgc/panely/internal/store"
)

// kayitSink, alarm olaylarını sırasıyla kaydeder.
type kayitSink struct{ olaylar []alarm.Event }

func (k *kayitSink) Notify(_ context.Context, ev alarm.Event) { k.olaylar = append(k.olaylar, ev) }

func gercekAlarmlar(t *testing.T) (*alarm.Manager, *store.Store, *kayitSink) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "panely.db"))
	if err != nil {
		t.Fatalf("veritabanı açılamadı: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	k := &kayitSink{}
	return alarm.New(db, k), db, k
}

func etkinAlarmlar(t *testing.T, db *store.Store) []store.Alarm {
	t.Helper()
	a, err := db.ListAlarms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestStartupAlarmIsClosedByTheProxyWatcher: reboot düzeltmesinin (K-112,
// 7. bulgu) dayandığı varsayım — açılışın açtığı "trafik akmıyor"
// alarmını vekil izleyicisi kapatabiliyor, çünkü İKİSİ AYNI KİMLİĞİ
// kullanıyor. Kimlikler ayrışırsa alarm yine SONSUZA DEK açık kalır ve
// izleyicinin kendi testleri bunu göremez: onlar sahte bir yöneticiyle
// yalnızca "Clear çağrıldı mı" diye bakıyor. Burada gerçek yönetici ve
// gerçek depo var.
func TestStartupAlarmIsClosedByTheProxyWatcher(t *testing.T) {
	ctx := context.Background()
	am, db, kayit := gercekAlarmlar(t)

	recordProxyAlarm(ctx, am, "rotalanamayan uygulama var: web: aktif sürümün (r1) ayakta replikası yok")
	acik := etkinAlarmlar(t, db)
	if len(acik) != 1 || acik[0].Severity != store.SeverityCritical {
		t.Fatalf("açılış alarmı açılmadı: %+v", acik)
	}

	// Gözetmen uygulamayı iyileştirdi, rota yüklendi: izleyici canlıyı
	// birebir ve atlanansız görüyor.
	w := &proxyWatcher{rp: &sahteOnarici{sonuclar: []onarimSonucu{{tam: true}}}, am: am}
	w.tick(ctx)

	if kalan := etkinAlarmlar(t, db); len(kalan) != 0 {
		t.Fatalf("izleyici açılış alarmını kapatamadı — kimlikler ayrışmış olabilir: %+v", kalan)
	}
	if len(kayit.olaylar) != 2 || kayit.olaylar[0].State != alarm.Opened || kayit.olaylar[1].State != alarm.Closed {
		t.Errorf("bildirim sırası açıldı → kapandı değil: %+v", kayit.olaylar)
	}
}

// TestStartupAlarmClearsOnACleanStartup: temiz bir açılış önceki
// açılıştan kalan alarmı kapatır.
func TestStartupAlarmClearsOnACleanStartup(t *testing.T) {
	ctx := context.Background()
	am, db, _ := gercekAlarmlar(t)

	recordProxyAlarm(ctx, am, "ters vekil uzlaştırılamadı: admin soketine ulaşılamadı")
	recordProxyAlarm(ctx, am, "")
	if kalan := etkinAlarmlar(t, db); len(kalan) != 0 {
		t.Errorf("temiz açılış alarmı kapatmadı: %+v", kalan)
	}
}
